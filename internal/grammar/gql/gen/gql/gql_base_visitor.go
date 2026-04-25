// Code generated from gql/GQL.g4 by ANTLR 4.13.1. DO NOT EDIT.

package gen // GQL
import "github.com/antlr4-go/antlr/v4"


type BaseGQLVisitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseGQLVisitor) VisitGqlProgram(ctx *GqlProgramContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitProgramActivity(ctx *ProgramActivityContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionActivity(ctx *SessionActivityContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTransactionActivity(ctx *TransactionActivityContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEndTransactionCommand(ctx *EndTransactionCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionSetCommand(ctx *SessionSetCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionSetSchemaClause(ctx *SessionSetSchemaClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionSetGraphClause(ctx *SessionSetGraphClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionSetTimeZoneClause(ctx *SessionSetTimeZoneClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSetTimeZoneValue(ctx *SetTimeZoneValueContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionSetParameterClause(ctx *SessionSetParameterClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionSetGraphParameterClause(ctx *SessionSetGraphParameterClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionSetBindingTableParameterClause(ctx *SessionSetBindingTableParameterClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionSetValueParameterClause(ctx *SessionSetValueParameterClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionSetParameterName(ctx *SessionSetParameterNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionResetCommand(ctx *SessionResetCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionResetArguments(ctx *SessionResetArgumentsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionCloseCommand(ctx *SessionCloseCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSessionParameterSpecification(ctx *SessionParameterSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitStartTransactionCommand(ctx *StartTransactionCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTransactionCharacteristics(ctx *TransactionCharacteristicsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTransactionMode(ctx *TransactionModeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTransactionAccessMode(ctx *TransactionAccessModeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRollbackCommand(ctx *RollbackCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCommitCommand(ctx *CommitCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNestedProcedureSpecification(ctx *NestedProcedureSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitProcedureSpecification(ctx *ProcedureSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNestedDataModifyingProcedureSpecification(ctx *NestedDataModifyingProcedureSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNestedQuerySpecification(ctx *NestedQuerySpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitProcedureBody(ctx *ProcedureBodyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingVariableDefinitionBlock(ctx *BindingVariableDefinitionBlockContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingVariableDefinition(ctx *BindingVariableDefinitionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitStatementBlock(ctx *StatementBlockContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitStatement(ctx *StatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNextStatement(ctx *NextStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphVariableDefinition(ctx *GraphVariableDefinitionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOptTypedGraphInitializer(ctx *OptTypedGraphInitializerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphInitializer(ctx *GraphInitializerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingTableVariableDefinition(ctx *BindingTableVariableDefinitionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOptTypedBindingTableInitializer(ctx *OptTypedBindingTableInitializerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingTableInitializer(ctx *BindingTableInitializerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitValueVariableDefinition(ctx *ValueVariableDefinitionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOptTypedValueInitializer(ctx *OptTypedValueInitializerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitValueInitializer(ctx *ValueInitializerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphExpression(ctx *GraphExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCurrentGraph(ctx *CurrentGraphContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingTableExpression(ctx *BindingTableExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNestedBindingTableQuerySpecification(ctx *NestedBindingTableQuerySpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitObjectExpressionPrimary(ctx *ObjectExpressionPrimaryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLinearCatalogModifyingStatement(ctx *LinearCatalogModifyingStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimpleCatalogModifyingStatement(ctx *SimpleCatalogModifyingStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPrimitiveCatalogModifyingStatement(ctx *PrimitiveCatalogModifyingStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCreateSchemaStatement(ctx *CreateSchemaStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDropSchemaStatement(ctx *DropSchemaStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCreateGraphStatement(ctx *CreateGraphStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOpenGraphType(ctx *OpenGraphTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOfGraphType(ctx *OfGraphTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphTypeLikeGraph(ctx *GraphTypeLikeGraphContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphSource(ctx *GraphSourceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDropGraphStatement(ctx *DropGraphStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCreateGraphTypeStatement(ctx *CreateGraphTypeStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphTypeSource(ctx *GraphTypeSourceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCopyOfGraphType(ctx *CopyOfGraphTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDropGraphTypeStatement(ctx *DropGraphTypeStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCallCatalogModifyingProcedureStatement(ctx *CallCatalogModifyingProcedureStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLinearDataModifyingStatement(ctx *LinearDataModifyingStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFocusedLinearDataModifyingStatement(ctx *FocusedLinearDataModifyingStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFocusedLinearDataModifyingStatementBody(ctx *FocusedLinearDataModifyingStatementBodyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFocusedNestedDataModifyingProcedureSpecification(ctx *FocusedNestedDataModifyingProcedureSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAmbientLinearDataModifyingStatement(ctx *AmbientLinearDataModifyingStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAmbientLinearDataModifyingStatementBody(ctx *AmbientLinearDataModifyingStatementBodyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimpleLinearDataAccessingStatement(ctx *SimpleLinearDataAccessingStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimpleDataAccessingStatement(ctx *SimpleDataAccessingStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimpleDataModifyingStatement(ctx *SimpleDataModifyingStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPrimitiveDataModifyingStatement(ctx *PrimitiveDataModifyingStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitInsertStatement(ctx *InsertStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSetStatement(ctx *SetStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSetItemList(ctx *SetItemListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSetItem(ctx *SetItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSetPropertyItem(ctx *SetPropertyItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSetAllPropertiesItem(ctx *SetAllPropertiesItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSetLabelItem(ctx *SetLabelItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRemoveStatement(ctx *RemoveStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRemoveItemList(ctx *RemoveItemListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRemoveItem(ctx *RemoveItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRemovePropertyItem(ctx *RemovePropertyItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRemoveLabelItem(ctx *RemoveLabelItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDeleteStatement(ctx *DeleteStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDeleteItemList(ctx *DeleteItemListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDeleteItem(ctx *DeleteItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCallDataModifyingProcedureStatement(ctx *CallDataModifyingProcedureStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCompositeQueryStatement(ctx *CompositeQueryStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCompositeQueryExpression(ctx *CompositeQueryExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitQueryConjunction(ctx *QueryConjunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSetOperator(ctx *SetOperatorContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCompositeQueryPrimary(ctx *CompositeQueryPrimaryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLinearQueryStatement(ctx *LinearQueryStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFocusedLinearQueryStatement(ctx *FocusedLinearQueryStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFocusedLinearQueryStatementPart(ctx *FocusedLinearQueryStatementPartContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFocusedLinearQueryAndPrimitiveResultStatementPart(ctx *FocusedLinearQueryAndPrimitiveResultStatementPartContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFocusedPrimitiveResultStatement(ctx *FocusedPrimitiveResultStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFocusedNestedQuerySpecification(ctx *FocusedNestedQuerySpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAmbientLinearQueryStatement(ctx *AmbientLinearQueryStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimpleLinearQueryStatement(ctx *SimpleLinearQueryStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimpleQueryStatement(ctx *SimpleQueryStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPrimitiveQueryStatement(ctx *PrimitiveQueryStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitMatchStatement(ctx *MatchStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimpleMatchStatement(ctx *SimpleMatchStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOptionalMatchStatement(ctx *OptionalMatchStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOptionalOperand(ctx *OptionalOperandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitMatchStatementBlock(ctx *MatchStatementBlockContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCallQueryStatement(ctx *CallQueryStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFilterStatement(ctx *FilterStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLetStatement(ctx *LetStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLetVariableDefinitionList(ctx *LetVariableDefinitionListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLetVariableDefinition(ctx *LetVariableDefinitionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitForStatement(ctx *ForStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitForItem(ctx *ForItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitForItemAlias(ctx *ForItemAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitForItemSource(ctx *ForItemSourceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitForOrdinalityOrOffset(ctx *ForOrdinalityOrOffsetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOrderByAndPageStatement(ctx *OrderByAndPageStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPrimitiveResultStatement(ctx *PrimitiveResultStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitReturnStatement(ctx *ReturnStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitReturnStatementBody(ctx *ReturnStatementBodyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitReturnItemList(ctx *ReturnItemListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitReturnItem(ctx *ReturnItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitReturnItemAlias(ctx *ReturnItemAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSelectStatement(ctx *SelectStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSelectItemList(ctx *SelectItemListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSelectItem(ctx *SelectItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSelectItemAlias(ctx *SelectItemAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitHavingClause(ctx *HavingClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSelectStatementBody(ctx *SelectStatementBodyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSelectGraphMatchList(ctx *SelectGraphMatchListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSelectGraphMatch(ctx *SelectGraphMatchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSelectQuerySpecification(ctx *SelectQuerySpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCallProcedureStatement(ctx *CallProcedureStatementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitProcedureCall(ctx *ProcedureCallContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitInlineProcedureCall(ctx *InlineProcedureCallContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitVariableScopeClause(ctx *VariableScopeClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingVariableReferenceList(ctx *BindingVariableReferenceListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNamedProcedureCall(ctx *NamedProcedureCallContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitProcedureArgumentList(ctx *ProcedureArgumentListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitProcedureArgument(ctx *ProcedureArgumentContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAtSchemaClause(ctx *AtSchemaClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitUseGraphClause(ctx *UseGraphClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphPatternBindingTable(ctx *GraphPatternBindingTableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphPatternYieldClause(ctx *GraphPatternYieldClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphPatternYieldItemList(ctx *GraphPatternYieldItemListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphPatternYieldItem(ctx *GraphPatternYieldItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphPattern(ctx *GraphPatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitMatchMode(ctx *MatchModeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRepeatableElementsMatchMode(ctx *RepeatableElementsMatchModeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDifferentEdgesMatchMode(ctx *DifferentEdgesMatchModeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementBindingsOrElements(ctx *ElementBindingsOrElementsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeBindingsOrEdges(ctx *EdgeBindingsOrEdgesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathPatternList(ctx *PathPatternListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathPattern(ctx *PathPatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathVariableDeclaration(ctx *PathVariableDeclarationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitKeepClause(ctx *KeepClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphPatternWhereClause(ctx *GraphPatternWhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitInsertGraphPattern(ctx *InsertGraphPatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitInsertPathPatternList(ctx *InsertPathPatternListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitInsertPathPattern(ctx *InsertPathPatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitInsertNodePattern(ctx *InsertNodePatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitInsertEdgePattern(ctx *InsertEdgePatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitInsertEdgePointingLeft(ctx *InsertEdgePointingLeftContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitInsertEdgePointingRight(ctx *InsertEdgePointingRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitInsertEdgeUndirected(ctx *InsertEdgeUndirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitInsertElementPatternFiller(ctx *InsertElementPatternFillerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabelAndPropertySetSpecification(ctx *LabelAndPropertySetSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathPatternPrefix(ctx *PathPatternPrefixContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathModePrefix(ctx *PathModePrefixContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathMode(ctx *PathModeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathSearchPrefix(ctx *PathSearchPrefixContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAllPathSearch(ctx *AllPathSearchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathOrPaths(ctx *PathOrPathsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAnyPathSearch(ctx *AnyPathSearchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNumberOfPaths(ctx *NumberOfPathsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitShortestPathSearch(ctx *ShortestPathSearchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAllShortestPathSearch(ctx *AllShortestPathSearchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAnyShortestPathSearch(ctx *AnyShortestPathSearchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCountedShortestPathSearch(ctx *CountedShortestPathSearchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCountedShortestGroupSearch(ctx *CountedShortestGroupSearchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNumberOfGroups(ctx *NumberOfGroupsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPpePathTerm(ctx *PpePathTermContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPpeMultisetAlternation(ctx *PpeMultisetAlternationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPpePatternUnion(ctx *PpePatternUnionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathTerm(ctx *PathTermContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPfPathPrimary(ctx *PfPathPrimaryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPfQuantifiedPathPrimary(ctx *PfQuantifiedPathPrimaryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPfQuestionedPathPrimary(ctx *PfQuestionedPathPrimaryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPpElementPattern(ctx *PpElementPatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPpParenthesizedPathPatternExpression(ctx *PpParenthesizedPathPatternExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPpSimplifiedPathPatternExpression(ctx *PpSimplifiedPathPatternExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementPattern(ctx *ElementPatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodePattern(ctx *NodePatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementPatternFiller(ctx *ElementPatternFillerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementVariableDeclaration(ctx *ElementVariableDeclarationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitIsLabelExpression(ctx *IsLabelExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitIsOrColon(ctx *IsOrColonContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementPatternPredicate(ctx *ElementPatternPredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementPatternWhereClause(ctx *ElementPatternWhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementPropertySpecification(ctx *ElementPropertySpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPropertyKeyValuePairList(ctx *PropertyKeyValuePairListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPropertyKeyValuePair(ctx *PropertyKeyValuePairContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgePattern(ctx *EdgePatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFullEdgePattern(ctx *FullEdgePatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFullEdgePointingLeft(ctx *FullEdgePointingLeftContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFullEdgeUndirected(ctx *FullEdgeUndirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFullEdgePointingRight(ctx *FullEdgePointingRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFullEdgeLeftOrUndirected(ctx *FullEdgeLeftOrUndirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFullEdgeUndirectedOrRight(ctx *FullEdgeUndirectedOrRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFullEdgeLeftOrRight(ctx *FullEdgeLeftOrRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFullEdgeAnyDirection(ctx *FullEdgeAnyDirectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAbbreviatedEdgePattern(ctx *AbbreviatedEdgePatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitParenthesizedPathPatternExpression(ctx *ParenthesizedPathPatternExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSubpathVariableDeclaration(ctx *SubpathVariableDeclarationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitParenthesizedPathPatternWhereClause(ctx *ParenthesizedPathPatternWhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabelExpressionNegation(ctx *LabelExpressionNegationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabelExpressionDisjunction(ctx *LabelExpressionDisjunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabelExpressionParenthesized(ctx *LabelExpressionParenthesizedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabelExpressionWildcard(ctx *LabelExpressionWildcardContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabelExpressionConjunction(ctx *LabelExpressionConjunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabelExpressionName(ctx *LabelExpressionNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathVariableReference(ctx *PathVariableReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementVariableReference(ctx *ElementVariableReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphPatternQuantifier(ctx *GraphPatternQuantifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFixedQuantifier(ctx *FixedQuantifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGeneralQuantifier(ctx *GeneralQuantifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLowerBound(ctx *LowerBoundContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitUpperBound(ctx *UpperBoundContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedPathPatternExpression(ctx *SimplifiedPathPatternExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedDefaultingLeft(ctx *SimplifiedDefaultingLeftContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedDefaultingUndirected(ctx *SimplifiedDefaultingUndirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedDefaultingRight(ctx *SimplifiedDefaultingRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedDefaultingLeftOrUndirected(ctx *SimplifiedDefaultingLeftOrUndirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedDefaultingUndirectedOrRight(ctx *SimplifiedDefaultingUndirectedOrRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedDefaultingLeftOrRight(ctx *SimplifiedDefaultingLeftOrRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedDefaultingAnyDirection(ctx *SimplifiedDefaultingAnyDirectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedContents(ctx *SimplifiedContentsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedPathUnion(ctx *SimplifiedPathUnionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedMultisetAlternation(ctx *SimplifiedMultisetAlternationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedFactorLowLabel(ctx *SimplifiedFactorLowLabelContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedConcatenationLabel(ctx *SimplifiedConcatenationLabelContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedConjunctionLabel(ctx *SimplifiedConjunctionLabelContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedFactorHighLabel(ctx *SimplifiedFactorHighLabelContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedFactorHigh(ctx *SimplifiedFactorHighContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedQuantified(ctx *SimplifiedQuantifiedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedQuestioned(ctx *SimplifiedQuestionedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedTertiary(ctx *SimplifiedTertiaryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedDirectionOverride(ctx *SimplifiedDirectionOverrideContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedOverrideLeft(ctx *SimplifiedOverrideLeftContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedOverrideUndirected(ctx *SimplifiedOverrideUndirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedOverrideRight(ctx *SimplifiedOverrideRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedOverrideLeftOrUndirected(ctx *SimplifiedOverrideLeftOrUndirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedOverrideUndirectedOrRight(ctx *SimplifiedOverrideUndirectedOrRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedOverrideLeftOrRight(ctx *SimplifiedOverrideLeftOrRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedOverrideAnyDirection(ctx *SimplifiedOverrideAnyDirectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedSecondary(ctx *SimplifiedSecondaryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedNegation(ctx *SimplifiedNegationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimplifiedPrimary(ctx *SimplifiedPrimaryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitWhereClause(ctx *WhereClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitYieldClause(ctx *YieldClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitYieldItemList(ctx *YieldItemListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitYieldItem(ctx *YieldItemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitYieldItemName(ctx *YieldItemNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitYieldItemAlias(ctx *YieldItemAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGroupByClause(ctx *GroupByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGroupingElementList(ctx *GroupingElementListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGroupingElement(ctx *GroupingElementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEmptyGroupingSet(ctx *EmptyGroupingSetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOrderByClause(ctx *OrderByClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSortSpecificationList(ctx *SortSpecificationListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSortSpecification(ctx *SortSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSortKey(ctx *SortKeyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOrderingSpecification(ctx *OrderingSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNullOrdering(ctx *NullOrderingContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLimitClause(ctx *LimitClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOffsetClause(ctx *OffsetClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOffsetSynonym(ctx *OffsetSynonymContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSchemaReference(ctx *SchemaReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAbsoluteCatalogSchemaReference(ctx *AbsoluteCatalogSchemaReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCatalogSchemaParentAndName(ctx *CatalogSchemaParentAndNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRelativeCatalogSchemaReference(ctx *RelativeCatalogSchemaReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPredefinedSchemaReference(ctx *PredefinedSchemaReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAbsoluteDirectoryPath(ctx *AbsoluteDirectoryPathContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRelativeDirectoryPath(ctx *RelativeDirectoryPathContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimpleDirectoryPath(ctx *SimpleDirectoryPathContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphReference(ctx *GraphReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCatalogGraphParentAndName(ctx *CatalogGraphParentAndNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitHomeGraph(ctx *HomeGraphContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphTypeReference(ctx *GraphTypeReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCatalogGraphTypeParentAndName(ctx *CatalogGraphTypeParentAndNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingTableReference(ctx *BindingTableReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitProcedureReference(ctx *ProcedureReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCatalogProcedureParentAndName(ctx *CatalogProcedureParentAndNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCatalogObjectParentReference(ctx *CatalogObjectParentReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitReferenceParameterSpecification(ctx *ReferenceParameterSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNestedGraphTypeSpecification(ctx *NestedGraphTypeSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphTypeSpecificationBody(ctx *GraphTypeSpecificationBodyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementTypeList(ctx *ElementTypeListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementTypeSpecification(ctx *ElementTypeSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeTypeSpecification(ctx *NodeTypeSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeTypePattern(ctx *NodeTypePatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeTypePhrase(ctx *NodeTypePhraseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeTypePhraseFiller(ctx *NodeTypePhraseFillerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeTypeFiller(ctx *NodeTypeFillerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLocalNodeTypeAlias(ctx *LocalNodeTypeAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeTypeImpliedContent(ctx *NodeTypeImpliedContentContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeTypeKeyLabelSet(ctx *NodeTypeKeyLabelSetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeTypeLabelSet(ctx *NodeTypeLabelSetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeTypePropertyTypes(ctx *NodeTypePropertyTypesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypeSpecification(ctx *EdgeTypeSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypePattern(ctx *EdgeTypePatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypePhrase(ctx *EdgeTypePhraseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypePhraseFiller(ctx *EdgeTypePhraseFillerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypeFiller(ctx *EdgeTypeFillerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypeImpliedContent(ctx *EdgeTypeImpliedContentContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypeKeyLabelSet(ctx *EdgeTypeKeyLabelSetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypeLabelSet(ctx *EdgeTypeLabelSetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypePropertyTypes(ctx *EdgeTypePropertyTypesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypePatternDirected(ctx *EdgeTypePatternDirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypePatternPointingRight(ctx *EdgeTypePatternPointingRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypePatternPointingLeft(ctx *EdgeTypePatternPointingLeftContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypePatternUndirected(ctx *EdgeTypePatternUndirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitArcTypePointingRight(ctx *ArcTypePointingRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitArcTypePointingLeft(ctx *ArcTypePointingLeftContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitArcTypeUndirected(ctx *ArcTypeUndirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSourceNodeTypeReference(ctx *SourceNodeTypeReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDestinationNodeTypeReference(ctx *DestinationNodeTypeReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeKind(ctx *EdgeKindContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEndpointPairPhrase(ctx *EndpointPairPhraseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEndpointPair(ctx *EndpointPairContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEndpointPairDirected(ctx *EndpointPairDirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEndpointPairPointingRight(ctx *EndpointPairPointingRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEndpointPairPointingLeft(ctx *EndpointPairPointingLeftContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEndpointPairUndirected(ctx *EndpointPairUndirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitConnectorPointingRight(ctx *ConnectorPointingRightContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitConnectorUndirected(ctx *ConnectorUndirectedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSourceNodeTypeAlias(ctx *SourceNodeTypeAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDestinationNodeTypeAlias(ctx *DestinationNodeTypeAliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabelSetPhrase(ctx *LabelSetPhraseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabelSetSpecification(ctx *LabelSetSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPropertyTypesSpecification(ctx *PropertyTypesSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPropertyTypeList(ctx *PropertyTypeListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPropertyType(ctx *PropertyTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPropertyValueType(ctx *PropertyValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingTableType(ctx *BindingTableTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDynamicPropertyValueTypeLabel(ctx *DynamicPropertyValueTypeLabelContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitClosedDynamicUnionTypeAtl1(ctx *ClosedDynamicUnionTypeAtl1Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitClosedDynamicUnionTypeAtl2(ctx *ClosedDynamicUnionTypeAtl2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathValueTypeLabel(ctx *PathValueTypeLabelContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListValueTypeAlt3(ctx *ListValueTypeAlt3Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListValueTypeAlt2(ctx *ListValueTypeAlt2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListValueTypeAlt1(ctx *ListValueTypeAlt1Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPredefinedTypeLabel(ctx *PredefinedTypeLabelContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRecordTypeLabel(ctx *RecordTypeLabelContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOpenDynamicUnionTypeLabel(ctx *OpenDynamicUnionTypeLabelContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTyped(ctx *TypedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPredefinedType(ctx *PredefinedTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBooleanType(ctx *BooleanTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCharacterStringType(ctx *CharacterStringTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitByteStringType(ctx *ByteStringTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitMinLength(ctx *MinLengthContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitMaxLength(ctx *MaxLengthContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFixedLength(ctx *FixedLengthContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNumericType(ctx *NumericTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitExactNumericType(ctx *ExactNumericTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBinaryExactNumericType(ctx *BinaryExactNumericTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSignedBinaryExactNumericType(ctx *SignedBinaryExactNumericTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitUnsignedBinaryExactNumericType(ctx *UnsignedBinaryExactNumericTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitVerboseBinaryExactNumericType(ctx *VerboseBinaryExactNumericTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDecimalExactNumericType(ctx *DecimalExactNumericTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPrecision(ctx *PrecisionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitScale(ctx *ScaleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitApproximateNumericType(ctx *ApproximateNumericTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTemporalType(ctx *TemporalTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTemporalInstantType(ctx *TemporalInstantTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDatetimeType(ctx *DatetimeTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLocaldatetimeType(ctx *LocaldatetimeTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDateType(ctx *DateTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTimeType(ctx *TimeTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLocaltimeType(ctx *LocaltimeTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTemporalDurationType(ctx *TemporalDurationTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTemporalDurationQualifier(ctx *TemporalDurationQualifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitReferenceValueType(ctx *ReferenceValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitImmaterialValueType(ctx *ImmaterialValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNullType(ctx *NullTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEmptyType(ctx *EmptyTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphReferenceValueType(ctx *GraphReferenceValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitClosedGraphReferenceValueType(ctx *ClosedGraphReferenceValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOpenGraphReferenceValueType(ctx *OpenGraphReferenceValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingTableReferenceValueType(ctx *BindingTableReferenceValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeReferenceValueType(ctx *NodeReferenceValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitClosedNodeReferenceValueType(ctx *ClosedNodeReferenceValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOpenNodeReferenceValueType(ctx *OpenNodeReferenceValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeReferenceValueType(ctx *EdgeReferenceValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitClosedEdgeReferenceValueType(ctx *ClosedEdgeReferenceValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitOpenEdgeReferenceValueType(ctx *OpenEdgeReferenceValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathValueType(ctx *PathValueTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListValueTypeName(ctx *ListValueTypeNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListValueTypeNameSynonym(ctx *ListValueTypeNameSynonymContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRecordType(ctx *RecordTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFieldTypesSpecification(ctx *FieldTypesSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFieldTypeList(ctx *FieldTypeListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNotNull(ctx *NotNullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFieldType(ctx *FieldTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSearchCondition(ctx *SearchConditionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPredicate(ctx *PredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCompOp(ctx *CompOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitExistsPredicate(ctx *ExistsPredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNullPredicate(ctx *NullPredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNullPredicatePart2(ctx *NullPredicatePart2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitValueTypePredicate(ctx *ValueTypePredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitValueTypePredicatePart2(ctx *ValueTypePredicatePart2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNormalizedPredicatePart2(ctx *NormalizedPredicatePart2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDirectedPredicate(ctx *DirectedPredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDirectedPredicatePart2(ctx *DirectedPredicatePart2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabeledPredicate(ctx *LabeledPredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabeledPredicatePart2(ctx *LabeledPredicatePart2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitIsLabeledOrColon(ctx *IsLabeledOrColonContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSourceDestinationPredicate(ctx *SourceDestinationPredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeReference(ctx *NodeReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSourcePredicatePart2(ctx *SourcePredicatePart2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDestinationPredicatePart2(ctx *DestinationPredicatePart2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeReference(ctx *EdgeReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAll_differentPredicate(ctx *All_differentPredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSamePredicate(ctx *SamePredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitProperty_existsPredicate(ctx *Property_existsPredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitConjunctiveExprAlt(ctx *ConjunctiveExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPropertyGraphExprAlt(ctx *PropertyGraphExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitMultDivExprAlt(ctx *MultDivExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingTableExprAlt(ctx *BindingTableExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSignedExprAlt(ctx *SignedExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitIsNotExprAlt(ctx *IsNotExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNormalizedPredicateExprAlt(ctx *NormalizedPredicateExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNotExprAlt(ctx *NotExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitValueFunctionExprAlt(ctx *ValueFunctionExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitConcatenationExprAlt(ctx *ConcatenationExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDisjunctiveExprAlt(ctx *DisjunctiveExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitComparisonExprAlt(ctx *ComparisonExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPrimaryExprAlt(ctx *PrimaryExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAddSubtractExprAlt(ctx *AddSubtractExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPredicateExprAlt(ctx *PredicateExprAltContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitValueFunction(ctx *ValueFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBooleanValueExpression(ctx *BooleanValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCharacterOrByteStringFunction(ctx *CharacterOrByteStringFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSubCharacterOrByteString(ctx *SubCharacterOrByteStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTrimSingleCharacterOrByteString(ctx *TrimSingleCharacterOrByteStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFoldCharacterString(ctx *FoldCharacterStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTrimMultiCharacterCharacterString(ctx *TrimMultiCharacterCharacterStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNormalizeCharacterString(ctx *NormalizeCharacterStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeReferenceValueExpression(ctx *NodeReferenceValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeReferenceValueExpression(ctx *EdgeReferenceValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAggregatingValueExpression(ctx *AggregatingValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitValueExpressionPrimary(ctx *ValueExpressionPrimaryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitParenthesizedValueExpression(ctx *ParenthesizedValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNonParenthesizedValueExpressionPrimary(ctx *NonParenthesizedValueExpressionPrimaryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNonParenthesizedValueExpressionPrimarySpecialCase(ctx *NonParenthesizedValueExpressionPrimarySpecialCaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitUnsignedValueSpecification(ctx *UnsignedValueSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNonNegativeIntegerSpecification(ctx *NonNegativeIntegerSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGeneralValueSpecification(ctx *GeneralValueSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDynamicParameterSpecification(ctx *DynamicParameterSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLetValueExpression(ctx *LetValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitValueQueryExpression(ctx *ValueQueryExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCaseExpression(ctx *CaseExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCaseAbbreviation(ctx *CaseAbbreviationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCaseSpecification(ctx *CaseSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimpleCase(ctx *SimpleCaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSearchedCase(ctx *SearchedCaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSimpleWhenClause(ctx *SimpleWhenClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSearchedWhenClause(ctx *SearchedWhenClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElseClause(ctx *ElseClauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCaseOperand(ctx *CaseOperandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitWhenOperandList(ctx *WhenOperandListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitWhenOperand(ctx *WhenOperandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitResult(ctx *ResultContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitResultExpression(ctx *ResultExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCastSpecification(ctx *CastSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCastOperand(ctx *CastOperandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCastTarget(ctx *CastTargetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAggregateFunction(ctx *AggregateFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGeneralSetFunction(ctx *GeneralSetFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBinarySetFunction(ctx *BinarySetFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGeneralSetFunctionType(ctx *GeneralSetFunctionTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSetQuantifier(ctx *SetQuantifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBinarySetFunctionType(ctx *BinarySetFunctionTypeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDependentValueExpression(ctx *DependentValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitIndependentValueExpression(ctx *IndependentValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElement_idFunction(ctx *Element_idFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingVariableReference(ctx *BindingVariableReferenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathValueExpression(ctx *PathValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathValueConstructor(ctx *PathValueConstructorContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathValueConstructorByEnumeration(ctx *PathValueConstructorByEnumerationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathElementList(ctx *PathElementListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathElementListStart(ctx *PathElementListStartContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathElementListStep(ctx *PathElementListStepContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListValueExpression(ctx *ListValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListValueFunction(ctx *ListValueFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTrimListFunction(ctx *TrimListFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementsFunction(ctx *ElementsFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListValueConstructor(ctx *ListValueConstructorContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListValueConstructorByEnumeration(ctx *ListValueConstructorByEnumerationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListElementList(ctx *ListElementListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListElement(ctx *ListElementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRecordConstructor(ctx *RecordConstructorContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFieldsSpecification(ctx *FieldsSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFieldList(ctx *FieldListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitField(ctx *FieldContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTruthValue(ctx *TruthValueContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNumericValueExpression(ctx *NumericValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNumericValueFunction(ctx *NumericValueFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLengthExpression(ctx *LengthExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCardinalityExpression(ctx *CardinalityExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCardinalityExpressionArgument(ctx *CardinalityExpressionArgumentContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCharLengthExpression(ctx *CharLengthExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitByteLengthExpression(ctx *ByteLengthExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathLengthExpression(ctx *PathLengthExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitAbsoluteValueExpression(ctx *AbsoluteValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitModulusExpression(ctx *ModulusExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNumericValueExpressionDividend(ctx *NumericValueExpressionDividendContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNumericValueExpressionDivisor(ctx *NumericValueExpressionDivisorContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTrigonometricFunction(ctx *TrigonometricFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTrigonometricFunctionName(ctx *TrigonometricFunctionNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGeneralLogarithmFunction(ctx *GeneralLogarithmFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGeneralLogarithmBase(ctx *GeneralLogarithmBaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGeneralLogarithmArgument(ctx *GeneralLogarithmArgumentContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCommonLogarithm(ctx *CommonLogarithmContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNaturalLogarithm(ctx *NaturalLogarithmContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitExponentialFunction(ctx *ExponentialFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPowerFunction(ctx *PowerFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNumericValueExpressionBase(ctx *NumericValueExpressionBaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNumericValueExpressionExponent(ctx *NumericValueExpressionExponentContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSquareRoot(ctx *SquareRootContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFloorFunction(ctx *FloorFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCeilingFunction(ctx *CeilingFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCharacterStringValueExpression(ctx *CharacterStringValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitByteStringValueExpression(ctx *ByteStringValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTrimOperands(ctx *TrimOperandsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTrimCharacterOrByteStringSource(ctx *TrimCharacterOrByteStringSourceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTrimSpecification(ctx *TrimSpecificationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTrimCharacterOrByteString(ctx *TrimCharacterOrByteStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNormalForm(ctx *NormalFormContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitStringLength(ctx *StringLengthContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDatetimeValueExpression(ctx *DatetimeValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDatetimeValueFunction(ctx *DatetimeValueFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDateFunction(ctx *DateFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTimeFunction(ctx *TimeFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLocaltimeFunction(ctx *LocaltimeFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDatetimeFunction(ctx *DatetimeFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLocaldatetimeFunction(ctx *LocaldatetimeFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDateFunctionParameters(ctx *DateFunctionParametersContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTimeFunctionParameters(ctx *TimeFunctionParametersContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDatetimeFunctionParameters(ctx *DatetimeFunctionParametersContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDurationValueExpression(ctx *DurationValueExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDatetimeSubtraction(ctx *DatetimeSubtractionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDatetimeSubtractionParameters(ctx *DatetimeSubtractionParametersContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDatetimeValueExpression1(ctx *DatetimeValueExpression1Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDatetimeValueExpression2(ctx *DatetimeValueExpression2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDurationValueFunction(ctx *DurationValueFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDurationFunction(ctx *DurationFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDurationFunctionParameters(ctx *DurationFunctionParametersContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitObjectName(ctx *ObjectNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitObjectNameOrBindingVariable(ctx *ObjectNameOrBindingVariableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDirectoryName(ctx *DirectoryNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSchemaName(ctx *SchemaNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphName(ctx *GraphNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDelimitedGraphName(ctx *DelimitedGraphNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGraphTypeName(ctx *GraphTypeNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeTypeName(ctx *NodeTypeNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeTypeName(ctx *EdgeTypeNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingTableName(ctx *BindingTableNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDelimitedBindingTableName(ctx *DelimitedBindingTableNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitProcedureName(ctx *ProcedureNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitLabelName(ctx *LabelNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPropertyName(ctx *PropertyNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitFieldName(ctx *FieldNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitElementVariable(ctx *ElementVariableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitPathVariable(ctx *PathVariableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitSubpathVariable(ctx *SubpathVariableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitBindingVariable(ctx *BindingVariableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitUnsignedLiteral(ctx *UnsignedLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitGeneralLiteral(ctx *GeneralLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTemporalLiteral(ctx *TemporalLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDateLiteral(ctx *DateLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTimeLiteral(ctx *TimeLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDatetimeLiteral(ctx *DatetimeLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitListLiteral(ctx *ListLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRecordLiteral(ctx *RecordLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitIdentifier(ctx *IdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitRegularIdentifier(ctx *RegularIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTimeZoneString(ctx *TimeZoneStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitCharacterStringLiteral(ctx *CharacterStringLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitUnsignedNumericLiteral(ctx *UnsignedNumericLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitExactNumericLiteral(ctx *ExactNumericLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitApproximateNumericLiteral(ctx *ApproximateNumericLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitUnsignedInteger(ctx *UnsignedIntegerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitUnsignedDecimalInteger(ctx *UnsignedDecimalIntegerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNullLiteral(ctx *NullLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDateString(ctx *DateStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitTimeString(ctx *TimeStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDatetimeString(ctx *DatetimeStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDurationLiteral(ctx *DurationLiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitDurationString(ctx *DurationStringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNodeSynonym(ctx *NodeSynonymContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgesSynonym(ctx *EdgesSynonymContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitEdgeSynonym(ctx *EdgeSynonymContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseGQLVisitor) VisitNonReservedWords(ctx *NonReservedWordsContext) interface{} {
	return v.VisitChildren(ctx)
}
