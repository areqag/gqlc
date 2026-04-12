// Code generated from GQL.g4 by ANTLR 4.13.1. DO NOT EDIT.

package gql // GQL
import "github.com/antlr4-go/antlr/v4"


// A complete Visitor for a parse tree produced by GQLParser.
type GQLVisitor interface {
	antlr.ParseTreeVisitor

	// Visit a parse tree produced by GQLParser#gqlProgram.
	VisitGqlProgram(ctx *GqlProgramContext) interface{}

	// Visit a parse tree produced by GQLParser#programActivity.
	VisitProgramActivity(ctx *ProgramActivityContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionActivity.
	VisitSessionActivity(ctx *SessionActivityContext) interface{}

	// Visit a parse tree produced by GQLParser#transactionActivity.
	VisitTransactionActivity(ctx *TransactionActivityContext) interface{}

	// Visit a parse tree produced by GQLParser#endTransactionCommand.
	VisitEndTransactionCommand(ctx *EndTransactionCommandContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionSetCommand.
	VisitSessionSetCommand(ctx *SessionSetCommandContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionSetSchemaClause.
	VisitSessionSetSchemaClause(ctx *SessionSetSchemaClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionSetGraphClause.
	VisitSessionSetGraphClause(ctx *SessionSetGraphClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionSetTimeZoneClause.
	VisitSessionSetTimeZoneClause(ctx *SessionSetTimeZoneClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#setTimeZoneValue.
	VisitSetTimeZoneValue(ctx *SetTimeZoneValueContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionSetParameterClause.
	VisitSessionSetParameterClause(ctx *SessionSetParameterClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionSetGraphParameterClause.
	VisitSessionSetGraphParameterClause(ctx *SessionSetGraphParameterClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionSetBindingTableParameterClause.
	VisitSessionSetBindingTableParameterClause(ctx *SessionSetBindingTableParameterClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionSetValueParameterClause.
	VisitSessionSetValueParameterClause(ctx *SessionSetValueParameterClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionSetParameterName.
	VisitSessionSetParameterName(ctx *SessionSetParameterNameContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionResetCommand.
	VisitSessionResetCommand(ctx *SessionResetCommandContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionResetArguments.
	VisitSessionResetArguments(ctx *SessionResetArgumentsContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionCloseCommand.
	VisitSessionCloseCommand(ctx *SessionCloseCommandContext) interface{}

	// Visit a parse tree produced by GQLParser#sessionParameterSpecification.
	VisitSessionParameterSpecification(ctx *SessionParameterSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#startTransactionCommand.
	VisitStartTransactionCommand(ctx *StartTransactionCommandContext) interface{}

	// Visit a parse tree produced by GQLParser#transactionCharacteristics.
	VisitTransactionCharacteristics(ctx *TransactionCharacteristicsContext) interface{}

	// Visit a parse tree produced by GQLParser#transactionMode.
	VisitTransactionMode(ctx *TransactionModeContext) interface{}

	// Visit a parse tree produced by GQLParser#transactionAccessMode.
	VisitTransactionAccessMode(ctx *TransactionAccessModeContext) interface{}

	// Visit a parse tree produced by GQLParser#rollbackCommand.
	VisitRollbackCommand(ctx *RollbackCommandContext) interface{}

	// Visit a parse tree produced by GQLParser#commitCommand.
	VisitCommitCommand(ctx *CommitCommandContext) interface{}

	// Visit a parse tree produced by GQLParser#nestedProcedureSpecification.
	VisitNestedProcedureSpecification(ctx *NestedProcedureSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#procedureSpecification.
	VisitProcedureSpecification(ctx *ProcedureSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#nestedDataModifyingProcedureSpecification.
	VisitNestedDataModifyingProcedureSpecification(ctx *NestedDataModifyingProcedureSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#nestedQuerySpecification.
	VisitNestedQuerySpecification(ctx *NestedQuerySpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#procedureBody.
	VisitProcedureBody(ctx *ProcedureBodyContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingVariableDefinitionBlock.
	VisitBindingVariableDefinitionBlock(ctx *BindingVariableDefinitionBlockContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingVariableDefinition.
	VisitBindingVariableDefinition(ctx *BindingVariableDefinitionContext) interface{}

	// Visit a parse tree produced by GQLParser#statementBlock.
	VisitStatementBlock(ctx *StatementBlockContext) interface{}

	// Visit a parse tree produced by GQLParser#statement.
	VisitStatement(ctx *StatementContext) interface{}

	// Visit a parse tree produced by GQLParser#nextStatement.
	VisitNextStatement(ctx *NextStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#graphVariableDefinition.
	VisitGraphVariableDefinition(ctx *GraphVariableDefinitionContext) interface{}

	// Visit a parse tree produced by GQLParser#optTypedGraphInitializer.
	VisitOptTypedGraphInitializer(ctx *OptTypedGraphInitializerContext) interface{}

	// Visit a parse tree produced by GQLParser#graphInitializer.
	VisitGraphInitializer(ctx *GraphInitializerContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingTableVariableDefinition.
	VisitBindingTableVariableDefinition(ctx *BindingTableVariableDefinitionContext) interface{}

	// Visit a parse tree produced by GQLParser#optTypedBindingTableInitializer.
	VisitOptTypedBindingTableInitializer(ctx *OptTypedBindingTableInitializerContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingTableInitializer.
	VisitBindingTableInitializer(ctx *BindingTableInitializerContext) interface{}

	// Visit a parse tree produced by GQLParser#valueVariableDefinition.
	VisitValueVariableDefinition(ctx *ValueVariableDefinitionContext) interface{}

	// Visit a parse tree produced by GQLParser#optTypedValueInitializer.
	VisitOptTypedValueInitializer(ctx *OptTypedValueInitializerContext) interface{}

	// Visit a parse tree produced by GQLParser#valueInitializer.
	VisitValueInitializer(ctx *ValueInitializerContext) interface{}

	// Visit a parse tree produced by GQLParser#graphExpression.
	VisitGraphExpression(ctx *GraphExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#currentGraph.
	VisitCurrentGraph(ctx *CurrentGraphContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingTableExpression.
	VisitBindingTableExpression(ctx *BindingTableExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#nestedBindingTableQuerySpecification.
	VisitNestedBindingTableQuerySpecification(ctx *NestedBindingTableQuerySpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#objectExpressionPrimary.
	VisitObjectExpressionPrimary(ctx *ObjectExpressionPrimaryContext) interface{}

	// Visit a parse tree produced by GQLParser#linearCatalogModifyingStatement.
	VisitLinearCatalogModifyingStatement(ctx *LinearCatalogModifyingStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#simpleCatalogModifyingStatement.
	VisitSimpleCatalogModifyingStatement(ctx *SimpleCatalogModifyingStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#primitiveCatalogModifyingStatement.
	VisitPrimitiveCatalogModifyingStatement(ctx *PrimitiveCatalogModifyingStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#createSchemaStatement.
	VisitCreateSchemaStatement(ctx *CreateSchemaStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#dropSchemaStatement.
	VisitDropSchemaStatement(ctx *DropSchemaStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#createGraphStatement.
	VisitCreateGraphStatement(ctx *CreateGraphStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#openGraphType.
	VisitOpenGraphType(ctx *OpenGraphTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#ofGraphType.
	VisitOfGraphType(ctx *OfGraphTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#graphTypeLikeGraph.
	VisitGraphTypeLikeGraph(ctx *GraphTypeLikeGraphContext) interface{}

	// Visit a parse tree produced by GQLParser#graphSource.
	VisitGraphSource(ctx *GraphSourceContext) interface{}

	// Visit a parse tree produced by GQLParser#dropGraphStatement.
	VisitDropGraphStatement(ctx *DropGraphStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#createGraphTypeStatement.
	VisitCreateGraphTypeStatement(ctx *CreateGraphTypeStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#graphTypeSource.
	VisitGraphTypeSource(ctx *GraphTypeSourceContext) interface{}

	// Visit a parse tree produced by GQLParser#copyOfGraphType.
	VisitCopyOfGraphType(ctx *CopyOfGraphTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#dropGraphTypeStatement.
	VisitDropGraphTypeStatement(ctx *DropGraphTypeStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#callCatalogModifyingProcedureStatement.
	VisitCallCatalogModifyingProcedureStatement(ctx *CallCatalogModifyingProcedureStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#linearDataModifyingStatement.
	VisitLinearDataModifyingStatement(ctx *LinearDataModifyingStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#focusedLinearDataModifyingStatement.
	VisitFocusedLinearDataModifyingStatement(ctx *FocusedLinearDataModifyingStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#focusedLinearDataModifyingStatementBody.
	VisitFocusedLinearDataModifyingStatementBody(ctx *FocusedLinearDataModifyingStatementBodyContext) interface{}

	// Visit a parse tree produced by GQLParser#focusedNestedDataModifyingProcedureSpecification.
	VisitFocusedNestedDataModifyingProcedureSpecification(ctx *FocusedNestedDataModifyingProcedureSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#ambientLinearDataModifyingStatement.
	VisitAmbientLinearDataModifyingStatement(ctx *AmbientLinearDataModifyingStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#ambientLinearDataModifyingStatementBody.
	VisitAmbientLinearDataModifyingStatementBody(ctx *AmbientLinearDataModifyingStatementBodyContext) interface{}

	// Visit a parse tree produced by GQLParser#simpleLinearDataAccessingStatement.
	VisitSimpleLinearDataAccessingStatement(ctx *SimpleLinearDataAccessingStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#simpleDataAccessingStatement.
	VisitSimpleDataAccessingStatement(ctx *SimpleDataAccessingStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#simpleDataModifyingStatement.
	VisitSimpleDataModifyingStatement(ctx *SimpleDataModifyingStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#primitiveDataModifyingStatement.
	VisitPrimitiveDataModifyingStatement(ctx *PrimitiveDataModifyingStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#insertStatement.
	VisitInsertStatement(ctx *InsertStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#setStatement.
	VisitSetStatement(ctx *SetStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#setItemList.
	VisitSetItemList(ctx *SetItemListContext) interface{}

	// Visit a parse tree produced by GQLParser#setItem.
	VisitSetItem(ctx *SetItemContext) interface{}

	// Visit a parse tree produced by GQLParser#setPropertyItem.
	VisitSetPropertyItem(ctx *SetPropertyItemContext) interface{}

	// Visit a parse tree produced by GQLParser#setAllPropertiesItem.
	VisitSetAllPropertiesItem(ctx *SetAllPropertiesItemContext) interface{}

	// Visit a parse tree produced by GQLParser#setLabelItem.
	VisitSetLabelItem(ctx *SetLabelItemContext) interface{}

	// Visit a parse tree produced by GQLParser#removeStatement.
	VisitRemoveStatement(ctx *RemoveStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#removeItemList.
	VisitRemoveItemList(ctx *RemoveItemListContext) interface{}

	// Visit a parse tree produced by GQLParser#removeItem.
	VisitRemoveItem(ctx *RemoveItemContext) interface{}

	// Visit a parse tree produced by GQLParser#removePropertyItem.
	VisitRemovePropertyItem(ctx *RemovePropertyItemContext) interface{}

	// Visit a parse tree produced by GQLParser#removeLabelItem.
	VisitRemoveLabelItem(ctx *RemoveLabelItemContext) interface{}

	// Visit a parse tree produced by GQLParser#deleteStatement.
	VisitDeleteStatement(ctx *DeleteStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#deleteItemList.
	VisitDeleteItemList(ctx *DeleteItemListContext) interface{}

	// Visit a parse tree produced by GQLParser#deleteItem.
	VisitDeleteItem(ctx *DeleteItemContext) interface{}

	// Visit a parse tree produced by GQLParser#callDataModifyingProcedureStatement.
	VisitCallDataModifyingProcedureStatement(ctx *CallDataModifyingProcedureStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#compositeQueryStatement.
	VisitCompositeQueryStatement(ctx *CompositeQueryStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#compositeQueryExpression.
	VisitCompositeQueryExpression(ctx *CompositeQueryExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#queryConjunction.
	VisitQueryConjunction(ctx *QueryConjunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#setOperator.
	VisitSetOperator(ctx *SetOperatorContext) interface{}

	// Visit a parse tree produced by GQLParser#compositeQueryPrimary.
	VisitCompositeQueryPrimary(ctx *CompositeQueryPrimaryContext) interface{}

	// Visit a parse tree produced by GQLParser#linearQueryStatement.
	VisitLinearQueryStatement(ctx *LinearQueryStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#focusedLinearQueryStatement.
	VisitFocusedLinearQueryStatement(ctx *FocusedLinearQueryStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#focusedLinearQueryStatementPart.
	VisitFocusedLinearQueryStatementPart(ctx *FocusedLinearQueryStatementPartContext) interface{}

	// Visit a parse tree produced by GQLParser#focusedLinearQueryAndPrimitiveResultStatementPart.
	VisitFocusedLinearQueryAndPrimitiveResultStatementPart(ctx *FocusedLinearQueryAndPrimitiveResultStatementPartContext) interface{}

	// Visit a parse tree produced by GQLParser#focusedPrimitiveResultStatement.
	VisitFocusedPrimitiveResultStatement(ctx *FocusedPrimitiveResultStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#focusedNestedQuerySpecification.
	VisitFocusedNestedQuerySpecification(ctx *FocusedNestedQuerySpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#ambientLinearQueryStatement.
	VisitAmbientLinearQueryStatement(ctx *AmbientLinearQueryStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#simpleLinearQueryStatement.
	VisitSimpleLinearQueryStatement(ctx *SimpleLinearQueryStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#simpleQueryStatement.
	VisitSimpleQueryStatement(ctx *SimpleQueryStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#primitiveQueryStatement.
	VisitPrimitiveQueryStatement(ctx *PrimitiveQueryStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#matchStatement.
	VisitMatchStatement(ctx *MatchStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#simpleMatchStatement.
	VisitSimpleMatchStatement(ctx *SimpleMatchStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#optionalMatchStatement.
	VisitOptionalMatchStatement(ctx *OptionalMatchStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#optionalOperand.
	VisitOptionalOperand(ctx *OptionalOperandContext) interface{}

	// Visit a parse tree produced by GQLParser#matchStatementBlock.
	VisitMatchStatementBlock(ctx *MatchStatementBlockContext) interface{}

	// Visit a parse tree produced by GQLParser#callQueryStatement.
	VisitCallQueryStatement(ctx *CallQueryStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#filterStatement.
	VisitFilterStatement(ctx *FilterStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#letStatement.
	VisitLetStatement(ctx *LetStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#letVariableDefinitionList.
	VisitLetVariableDefinitionList(ctx *LetVariableDefinitionListContext) interface{}

	// Visit a parse tree produced by GQLParser#letVariableDefinition.
	VisitLetVariableDefinition(ctx *LetVariableDefinitionContext) interface{}

	// Visit a parse tree produced by GQLParser#forStatement.
	VisitForStatement(ctx *ForStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#forItem.
	VisitForItem(ctx *ForItemContext) interface{}

	// Visit a parse tree produced by GQLParser#forItemAlias.
	VisitForItemAlias(ctx *ForItemAliasContext) interface{}

	// Visit a parse tree produced by GQLParser#forItemSource.
	VisitForItemSource(ctx *ForItemSourceContext) interface{}

	// Visit a parse tree produced by GQLParser#forOrdinalityOrOffset.
	VisitForOrdinalityOrOffset(ctx *ForOrdinalityOrOffsetContext) interface{}

	// Visit a parse tree produced by GQLParser#orderByAndPageStatement.
	VisitOrderByAndPageStatement(ctx *OrderByAndPageStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#primitiveResultStatement.
	VisitPrimitiveResultStatement(ctx *PrimitiveResultStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#returnStatement.
	VisitReturnStatement(ctx *ReturnStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#returnStatementBody.
	VisitReturnStatementBody(ctx *ReturnStatementBodyContext) interface{}

	// Visit a parse tree produced by GQLParser#returnItemList.
	VisitReturnItemList(ctx *ReturnItemListContext) interface{}

	// Visit a parse tree produced by GQLParser#returnItem.
	VisitReturnItem(ctx *ReturnItemContext) interface{}

	// Visit a parse tree produced by GQLParser#returnItemAlias.
	VisitReturnItemAlias(ctx *ReturnItemAliasContext) interface{}

	// Visit a parse tree produced by GQLParser#selectStatement.
	VisitSelectStatement(ctx *SelectStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#selectItemList.
	VisitSelectItemList(ctx *SelectItemListContext) interface{}

	// Visit a parse tree produced by GQLParser#selectItem.
	VisitSelectItem(ctx *SelectItemContext) interface{}

	// Visit a parse tree produced by GQLParser#selectItemAlias.
	VisitSelectItemAlias(ctx *SelectItemAliasContext) interface{}

	// Visit a parse tree produced by GQLParser#havingClause.
	VisitHavingClause(ctx *HavingClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#selectStatementBody.
	VisitSelectStatementBody(ctx *SelectStatementBodyContext) interface{}

	// Visit a parse tree produced by GQLParser#selectGraphMatchList.
	VisitSelectGraphMatchList(ctx *SelectGraphMatchListContext) interface{}

	// Visit a parse tree produced by GQLParser#selectGraphMatch.
	VisitSelectGraphMatch(ctx *SelectGraphMatchContext) interface{}

	// Visit a parse tree produced by GQLParser#selectQuerySpecification.
	VisitSelectQuerySpecification(ctx *SelectQuerySpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#callProcedureStatement.
	VisitCallProcedureStatement(ctx *CallProcedureStatementContext) interface{}

	// Visit a parse tree produced by GQLParser#procedureCall.
	VisitProcedureCall(ctx *ProcedureCallContext) interface{}

	// Visit a parse tree produced by GQLParser#inlineProcedureCall.
	VisitInlineProcedureCall(ctx *InlineProcedureCallContext) interface{}

	// Visit a parse tree produced by GQLParser#variableScopeClause.
	VisitVariableScopeClause(ctx *VariableScopeClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingVariableReferenceList.
	VisitBindingVariableReferenceList(ctx *BindingVariableReferenceListContext) interface{}

	// Visit a parse tree produced by GQLParser#namedProcedureCall.
	VisitNamedProcedureCall(ctx *NamedProcedureCallContext) interface{}

	// Visit a parse tree produced by GQLParser#procedureArgumentList.
	VisitProcedureArgumentList(ctx *ProcedureArgumentListContext) interface{}

	// Visit a parse tree produced by GQLParser#procedureArgument.
	VisitProcedureArgument(ctx *ProcedureArgumentContext) interface{}

	// Visit a parse tree produced by GQLParser#atSchemaClause.
	VisitAtSchemaClause(ctx *AtSchemaClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#useGraphClause.
	VisitUseGraphClause(ctx *UseGraphClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#graphPatternBindingTable.
	VisitGraphPatternBindingTable(ctx *GraphPatternBindingTableContext) interface{}

	// Visit a parse tree produced by GQLParser#graphPatternYieldClause.
	VisitGraphPatternYieldClause(ctx *GraphPatternYieldClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#graphPatternYieldItemList.
	VisitGraphPatternYieldItemList(ctx *GraphPatternYieldItemListContext) interface{}

	// Visit a parse tree produced by GQLParser#graphPatternYieldItem.
	VisitGraphPatternYieldItem(ctx *GraphPatternYieldItemContext) interface{}

	// Visit a parse tree produced by GQLParser#graphPattern.
	VisitGraphPattern(ctx *GraphPatternContext) interface{}

	// Visit a parse tree produced by GQLParser#matchMode.
	VisitMatchMode(ctx *MatchModeContext) interface{}

	// Visit a parse tree produced by GQLParser#repeatableElementsMatchMode.
	VisitRepeatableElementsMatchMode(ctx *RepeatableElementsMatchModeContext) interface{}

	// Visit a parse tree produced by GQLParser#differentEdgesMatchMode.
	VisitDifferentEdgesMatchMode(ctx *DifferentEdgesMatchModeContext) interface{}

	// Visit a parse tree produced by GQLParser#elementBindingsOrElements.
	VisitElementBindingsOrElements(ctx *ElementBindingsOrElementsContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeBindingsOrEdges.
	VisitEdgeBindingsOrEdges(ctx *EdgeBindingsOrEdgesContext) interface{}

	// Visit a parse tree produced by GQLParser#pathPatternList.
	VisitPathPatternList(ctx *PathPatternListContext) interface{}

	// Visit a parse tree produced by GQLParser#pathPattern.
	VisitPathPattern(ctx *PathPatternContext) interface{}

	// Visit a parse tree produced by GQLParser#pathVariableDeclaration.
	VisitPathVariableDeclaration(ctx *PathVariableDeclarationContext) interface{}

	// Visit a parse tree produced by GQLParser#keepClause.
	VisitKeepClause(ctx *KeepClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#graphPatternWhereClause.
	VisitGraphPatternWhereClause(ctx *GraphPatternWhereClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#insertGraphPattern.
	VisitInsertGraphPattern(ctx *InsertGraphPatternContext) interface{}

	// Visit a parse tree produced by GQLParser#insertPathPatternList.
	VisitInsertPathPatternList(ctx *InsertPathPatternListContext) interface{}

	// Visit a parse tree produced by GQLParser#insertPathPattern.
	VisitInsertPathPattern(ctx *InsertPathPatternContext) interface{}

	// Visit a parse tree produced by GQLParser#insertNodePattern.
	VisitInsertNodePattern(ctx *InsertNodePatternContext) interface{}

	// Visit a parse tree produced by GQLParser#insertEdgePattern.
	VisitInsertEdgePattern(ctx *InsertEdgePatternContext) interface{}

	// Visit a parse tree produced by GQLParser#insertEdgePointingLeft.
	VisitInsertEdgePointingLeft(ctx *InsertEdgePointingLeftContext) interface{}

	// Visit a parse tree produced by GQLParser#insertEdgePointingRight.
	VisitInsertEdgePointingRight(ctx *InsertEdgePointingRightContext) interface{}

	// Visit a parse tree produced by GQLParser#insertEdgeUndirected.
	VisitInsertEdgeUndirected(ctx *InsertEdgeUndirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#insertElementPatternFiller.
	VisitInsertElementPatternFiller(ctx *InsertElementPatternFillerContext) interface{}

	// Visit a parse tree produced by GQLParser#labelAndPropertySetSpecification.
	VisitLabelAndPropertySetSpecification(ctx *LabelAndPropertySetSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#pathPatternPrefix.
	VisitPathPatternPrefix(ctx *PathPatternPrefixContext) interface{}

	// Visit a parse tree produced by GQLParser#pathModePrefix.
	VisitPathModePrefix(ctx *PathModePrefixContext) interface{}

	// Visit a parse tree produced by GQLParser#pathMode.
	VisitPathMode(ctx *PathModeContext) interface{}

	// Visit a parse tree produced by GQLParser#pathSearchPrefix.
	VisitPathSearchPrefix(ctx *PathSearchPrefixContext) interface{}

	// Visit a parse tree produced by GQLParser#allPathSearch.
	VisitAllPathSearch(ctx *AllPathSearchContext) interface{}

	// Visit a parse tree produced by GQLParser#pathOrPaths.
	VisitPathOrPaths(ctx *PathOrPathsContext) interface{}

	// Visit a parse tree produced by GQLParser#anyPathSearch.
	VisitAnyPathSearch(ctx *AnyPathSearchContext) interface{}

	// Visit a parse tree produced by GQLParser#numberOfPaths.
	VisitNumberOfPaths(ctx *NumberOfPathsContext) interface{}

	// Visit a parse tree produced by GQLParser#shortestPathSearch.
	VisitShortestPathSearch(ctx *ShortestPathSearchContext) interface{}

	// Visit a parse tree produced by GQLParser#allShortestPathSearch.
	VisitAllShortestPathSearch(ctx *AllShortestPathSearchContext) interface{}

	// Visit a parse tree produced by GQLParser#anyShortestPathSearch.
	VisitAnyShortestPathSearch(ctx *AnyShortestPathSearchContext) interface{}

	// Visit a parse tree produced by GQLParser#countedShortestPathSearch.
	VisitCountedShortestPathSearch(ctx *CountedShortestPathSearchContext) interface{}

	// Visit a parse tree produced by GQLParser#countedShortestGroupSearch.
	VisitCountedShortestGroupSearch(ctx *CountedShortestGroupSearchContext) interface{}

	// Visit a parse tree produced by GQLParser#numberOfGroups.
	VisitNumberOfGroups(ctx *NumberOfGroupsContext) interface{}

	// Visit a parse tree produced by GQLParser#ppePathTerm.
	VisitPpePathTerm(ctx *PpePathTermContext) interface{}

	// Visit a parse tree produced by GQLParser#ppeMultisetAlternation.
	VisitPpeMultisetAlternation(ctx *PpeMultisetAlternationContext) interface{}

	// Visit a parse tree produced by GQLParser#ppePatternUnion.
	VisitPpePatternUnion(ctx *PpePatternUnionContext) interface{}

	// Visit a parse tree produced by GQLParser#pathTerm.
	VisitPathTerm(ctx *PathTermContext) interface{}

	// Visit a parse tree produced by GQLParser#pfPathPrimary.
	VisitPfPathPrimary(ctx *PfPathPrimaryContext) interface{}

	// Visit a parse tree produced by GQLParser#pfQuantifiedPathPrimary.
	VisitPfQuantifiedPathPrimary(ctx *PfQuantifiedPathPrimaryContext) interface{}

	// Visit a parse tree produced by GQLParser#pfQuestionedPathPrimary.
	VisitPfQuestionedPathPrimary(ctx *PfQuestionedPathPrimaryContext) interface{}

	// Visit a parse tree produced by GQLParser#ppElementPattern.
	VisitPpElementPattern(ctx *PpElementPatternContext) interface{}

	// Visit a parse tree produced by GQLParser#ppParenthesizedPathPatternExpression.
	VisitPpParenthesizedPathPatternExpression(ctx *PpParenthesizedPathPatternExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#ppSimplifiedPathPatternExpression.
	VisitPpSimplifiedPathPatternExpression(ctx *PpSimplifiedPathPatternExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#elementPattern.
	VisitElementPattern(ctx *ElementPatternContext) interface{}

	// Visit a parse tree produced by GQLParser#nodePattern.
	VisitNodePattern(ctx *NodePatternContext) interface{}

	// Visit a parse tree produced by GQLParser#elementPatternFiller.
	VisitElementPatternFiller(ctx *ElementPatternFillerContext) interface{}

	// Visit a parse tree produced by GQLParser#elementVariableDeclaration.
	VisitElementVariableDeclaration(ctx *ElementVariableDeclarationContext) interface{}

	// Visit a parse tree produced by GQLParser#isLabelExpression.
	VisitIsLabelExpression(ctx *IsLabelExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#isOrColon.
	VisitIsOrColon(ctx *IsOrColonContext) interface{}

	// Visit a parse tree produced by GQLParser#elementPatternPredicate.
	VisitElementPatternPredicate(ctx *ElementPatternPredicateContext) interface{}

	// Visit a parse tree produced by GQLParser#elementPatternWhereClause.
	VisitElementPatternWhereClause(ctx *ElementPatternWhereClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#elementPropertySpecification.
	VisitElementPropertySpecification(ctx *ElementPropertySpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#propertyKeyValuePairList.
	VisitPropertyKeyValuePairList(ctx *PropertyKeyValuePairListContext) interface{}

	// Visit a parse tree produced by GQLParser#propertyKeyValuePair.
	VisitPropertyKeyValuePair(ctx *PropertyKeyValuePairContext) interface{}

	// Visit a parse tree produced by GQLParser#edgePattern.
	VisitEdgePattern(ctx *EdgePatternContext) interface{}

	// Visit a parse tree produced by GQLParser#fullEdgePattern.
	VisitFullEdgePattern(ctx *FullEdgePatternContext) interface{}

	// Visit a parse tree produced by GQLParser#fullEdgePointingLeft.
	VisitFullEdgePointingLeft(ctx *FullEdgePointingLeftContext) interface{}

	// Visit a parse tree produced by GQLParser#fullEdgeUndirected.
	VisitFullEdgeUndirected(ctx *FullEdgeUndirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#fullEdgePointingRight.
	VisitFullEdgePointingRight(ctx *FullEdgePointingRightContext) interface{}

	// Visit a parse tree produced by GQLParser#fullEdgeLeftOrUndirected.
	VisitFullEdgeLeftOrUndirected(ctx *FullEdgeLeftOrUndirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#fullEdgeUndirectedOrRight.
	VisitFullEdgeUndirectedOrRight(ctx *FullEdgeUndirectedOrRightContext) interface{}

	// Visit a parse tree produced by GQLParser#fullEdgeLeftOrRight.
	VisitFullEdgeLeftOrRight(ctx *FullEdgeLeftOrRightContext) interface{}

	// Visit a parse tree produced by GQLParser#fullEdgeAnyDirection.
	VisitFullEdgeAnyDirection(ctx *FullEdgeAnyDirectionContext) interface{}

	// Visit a parse tree produced by GQLParser#abbreviatedEdgePattern.
	VisitAbbreviatedEdgePattern(ctx *AbbreviatedEdgePatternContext) interface{}

	// Visit a parse tree produced by GQLParser#parenthesizedPathPatternExpression.
	VisitParenthesizedPathPatternExpression(ctx *ParenthesizedPathPatternExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#subpathVariableDeclaration.
	VisitSubpathVariableDeclaration(ctx *SubpathVariableDeclarationContext) interface{}

	// Visit a parse tree produced by GQLParser#parenthesizedPathPatternWhereClause.
	VisitParenthesizedPathPatternWhereClause(ctx *ParenthesizedPathPatternWhereClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#labelExpressionNegation.
	VisitLabelExpressionNegation(ctx *LabelExpressionNegationContext) interface{}

	// Visit a parse tree produced by GQLParser#labelExpressionDisjunction.
	VisitLabelExpressionDisjunction(ctx *LabelExpressionDisjunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#labelExpressionParenthesized.
	VisitLabelExpressionParenthesized(ctx *LabelExpressionParenthesizedContext) interface{}

	// Visit a parse tree produced by GQLParser#labelExpressionWildcard.
	VisitLabelExpressionWildcard(ctx *LabelExpressionWildcardContext) interface{}

	// Visit a parse tree produced by GQLParser#labelExpressionConjunction.
	VisitLabelExpressionConjunction(ctx *LabelExpressionConjunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#labelExpressionName.
	VisitLabelExpressionName(ctx *LabelExpressionNameContext) interface{}

	// Visit a parse tree produced by GQLParser#pathVariableReference.
	VisitPathVariableReference(ctx *PathVariableReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#elementVariableReference.
	VisitElementVariableReference(ctx *ElementVariableReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#graphPatternQuantifier.
	VisitGraphPatternQuantifier(ctx *GraphPatternQuantifierContext) interface{}

	// Visit a parse tree produced by GQLParser#fixedQuantifier.
	VisitFixedQuantifier(ctx *FixedQuantifierContext) interface{}

	// Visit a parse tree produced by GQLParser#generalQuantifier.
	VisitGeneralQuantifier(ctx *GeneralQuantifierContext) interface{}

	// Visit a parse tree produced by GQLParser#lowerBound.
	VisitLowerBound(ctx *LowerBoundContext) interface{}

	// Visit a parse tree produced by GQLParser#upperBound.
	VisitUpperBound(ctx *UpperBoundContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedPathPatternExpression.
	VisitSimplifiedPathPatternExpression(ctx *SimplifiedPathPatternExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedDefaultingLeft.
	VisitSimplifiedDefaultingLeft(ctx *SimplifiedDefaultingLeftContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedDefaultingUndirected.
	VisitSimplifiedDefaultingUndirected(ctx *SimplifiedDefaultingUndirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedDefaultingRight.
	VisitSimplifiedDefaultingRight(ctx *SimplifiedDefaultingRightContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedDefaultingLeftOrUndirected.
	VisitSimplifiedDefaultingLeftOrUndirected(ctx *SimplifiedDefaultingLeftOrUndirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedDefaultingUndirectedOrRight.
	VisitSimplifiedDefaultingUndirectedOrRight(ctx *SimplifiedDefaultingUndirectedOrRightContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedDefaultingLeftOrRight.
	VisitSimplifiedDefaultingLeftOrRight(ctx *SimplifiedDefaultingLeftOrRightContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedDefaultingAnyDirection.
	VisitSimplifiedDefaultingAnyDirection(ctx *SimplifiedDefaultingAnyDirectionContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedContents.
	VisitSimplifiedContents(ctx *SimplifiedContentsContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedPathUnion.
	VisitSimplifiedPathUnion(ctx *SimplifiedPathUnionContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedMultisetAlternation.
	VisitSimplifiedMultisetAlternation(ctx *SimplifiedMultisetAlternationContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedFactorLowLabel.
	VisitSimplifiedFactorLowLabel(ctx *SimplifiedFactorLowLabelContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedConcatenationLabel.
	VisitSimplifiedConcatenationLabel(ctx *SimplifiedConcatenationLabelContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedConjunctionLabel.
	VisitSimplifiedConjunctionLabel(ctx *SimplifiedConjunctionLabelContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedFactorHighLabel.
	VisitSimplifiedFactorHighLabel(ctx *SimplifiedFactorHighLabelContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedFactorHigh.
	VisitSimplifiedFactorHigh(ctx *SimplifiedFactorHighContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedQuantified.
	VisitSimplifiedQuantified(ctx *SimplifiedQuantifiedContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedQuestioned.
	VisitSimplifiedQuestioned(ctx *SimplifiedQuestionedContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedTertiary.
	VisitSimplifiedTertiary(ctx *SimplifiedTertiaryContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedDirectionOverride.
	VisitSimplifiedDirectionOverride(ctx *SimplifiedDirectionOverrideContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedOverrideLeft.
	VisitSimplifiedOverrideLeft(ctx *SimplifiedOverrideLeftContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedOverrideUndirected.
	VisitSimplifiedOverrideUndirected(ctx *SimplifiedOverrideUndirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedOverrideRight.
	VisitSimplifiedOverrideRight(ctx *SimplifiedOverrideRightContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedOverrideLeftOrUndirected.
	VisitSimplifiedOverrideLeftOrUndirected(ctx *SimplifiedOverrideLeftOrUndirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedOverrideUndirectedOrRight.
	VisitSimplifiedOverrideUndirectedOrRight(ctx *SimplifiedOverrideUndirectedOrRightContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedOverrideLeftOrRight.
	VisitSimplifiedOverrideLeftOrRight(ctx *SimplifiedOverrideLeftOrRightContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedOverrideAnyDirection.
	VisitSimplifiedOverrideAnyDirection(ctx *SimplifiedOverrideAnyDirectionContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedSecondary.
	VisitSimplifiedSecondary(ctx *SimplifiedSecondaryContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedNegation.
	VisitSimplifiedNegation(ctx *SimplifiedNegationContext) interface{}

	// Visit a parse tree produced by GQLParser#simplifiedPrimary.
	VisitSimplifiedPrimary(ctx *SimplifiedPrimaryContext) interface{}

	// Visit a parse tree produced by GQLParser#whereClause.
	VisitWhereClause(ctx *WhereClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#yieldClause.
	VisitYieldClause(ctx *YieldClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#yieldItemList.
	VisitYieldItemList(ctx *YieldItemListContext) interface{}

	// Visit a parse tree produced by GQLParser#yieldItem.
	VisitYieldItem(ctx *YieldItemContext) interface{}

	// Visit a parse tree produced by GQLParser#yieldItemName.
	VisitYieldItemName(ctx *YieldItemNameContext) interface{}

	// Visit a parse tree produced by GQLParser#yieldItemAlias.
	VisitYieldItemAlias(ctx *YieldItemAliasContext) interface{}

	// Visit a parse tree produced by GQLParser#groupByClause.
	VisitGroupByClause(ctx *GroupByClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#groupingElementList.
	VisitGroupingElementList(ctx *GroupingElementListContext) interface{}

	// Visit a parse tree produced by GQLParser#groupingElement.
	VisitGroupingElement(ctx *GroupingElementContext) interface{}

	// Visit a parse tree produced by GQLParser#emptyGroupingSet.
	VisitEmptyGroupingSet(ctx *EmptyGroupingSetContext) interface{}

	// Visit a parse tree produced by GQLParser#orderByClause.
	VisitOrderByClause(ctx *OrderByClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#sortSpecificationList.
	VisitSortSpecificationList(ctx *SortSpecificationListContext) interface{}

	// Visit a parse tree produced by GQLParser#sortSpecification.
	VisitSortSpecification(ctx *SortSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#sortKey.
	VisitSortKey(ctx *SortKeyContext) interface{}

	// Visit a parse tree produced by GQLParser#orderingSpecification.
	VisitOrderingSpecification(ctx *OrderingSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#nullOrdering.
	VisitNullOrdering(ctx *NullOrderingContext) interface{}

	// Visit a parse tree produced by GQLParser#limitClause.
	VisitLimitClause(ctx *LimitClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#offsetClause.
	VisitOffsetClause(ctx *OffsetClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#offsetSynonym.
	VisitOffsetSynonym(ctx *OffsetSynonymContext) interface{}

	// Visit a parse tree produced by GQLParser#schemaReference.
	VisitSchemaReference(ctx *SchemaReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#absoluteCatalogSchemaReference.
	VisitAbsoluteCatalogSchemaReference(ctx *AbsoluteCatalogSchemaReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#catalogSchemaParentAndName.
	VisitCatalogSchemaParentAndName(ctx *CatalogSchemaParentAndNameContext) interface{}

	// Visit a parse tree produced by GQLParser#relativeCatalogSchemaReference.
	VisitRelativeCatalogSchemaReference(ctx *RelativeCatalogSchemaReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#predefinedSchemaReference.
	VisitPredefinedSchemaReference(ctx *PredefinedSchemaReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#absoluteDirectoryPath.
	VisitAbsoluteDirectoryPath(ctx *AbsoluteDirectoryPathContext) interface{}

	// Visit a parse tree produced by GQLParser#relativeDirectoryPath.
	VisitRelativeDirectoryPath(ctx *RelativeDirectoryPathContext) interface{}

	// Visit a parse tree produced by GQLParser#simpleDirectoryPath.
	VisitSimpleDirectoryPath(ctx *SimpleDirectoryPathContext) interface{}

	// Visit a parse tree produced by GQLParser#graphReference.
	VisitGraphReference(ctx *GraphReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#catalogGraphParentAndName.
	VisitCatalogGraphParentAndName(ctx *CatalogGraphParentAndNameContext) interface{}

	// Visit a parse tree produced by GQLParser#homeGraph.
	VisitHomeGraph(ctx *HomeGraphContext) interface{}

	// Visit a parse tree produced by GQLParser#graphTypeReference.
	VisitGraphTypeReference(ctx *GraphTypeReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#catalogGraphTypeParentAndName.
	VisitCatalogGraphTypeParentAndName(ctx *CatalogGraphTypeParentAndNameContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingTableReference.
	VisitBindingTableReference(ctx *BindingTableReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#procedureReference.
	VisitProcedureReference(ctx *ProcedureReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#catalogProcedureParentAndName.
	VisitCatalogProcedureParentAndName(ctx *CatalogProcedureParentAndNameContext) interface{}

	// Visit a parse tree produced by GQLParser#catalogObjectParentReference.
	VisitCatalogObjectParentReference(ctx *CatalogObjectParentReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#referenceParameterSpecification.
	VisitReferenceParameterSpecification(ctx *ReferenceParameterSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#nestedGraphTypeSpecification.
	VisitNestedGraphTypeSpecification(ctx *NestedGraphTypeSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#graphTypeSpecificationBody.
	VisitGraphTypeSpecificationBody(ctx *GraphTypeSpecificationBodyContext) interface{}

	// Visit a parse tree produced by GQLParser#elementTypeList.
	VisitElementTypeList(ctx *ElementTypeListContext) interface{}

	// Visit a parse tree produced by GQLParser#elementTypeSpecification.
	VisitElementTypeSpecification(ctx *ElementTypeSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeTypeSpecification.
	VisitNodeTypeSpecification(ctx *NodeTypeSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeTypePattern.
	VisitNodeTypePattern(ctx *NodeTypePatternContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeTypePhrase.
	VisitNodeTypePhrase(ctx *NodeTypePhraseContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeTypePhraseFiller.
	VisitNodeTypePhraseFiller(ctx *NodeTypePhraseFillerContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeTypeFiller.
	VisitNodeTypeFiller(ctx *NodeTypeFillerContext) interface{}

	// Visit a parse tree produced by GQLParser#localNodeTypeAlias.
	VisitLocalNodeTypeAlias(ctx *LocalNodeTypeAliasContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeTypeImpliedContent.
	VisitNodeTypeImpliedContent(ctx *NodeTypeImpliedContentContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeTypeKeyLabelSet.
	VisitNodeTypeKeyLabelSet(ctx *NodeTypeKeyLabelSetContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeTypeLabelSet.
	VisitNodeTypeLabelSet(ctx *NodeTypeLabelSetContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeTypePropertyTypes.
	VisitNodeTypePropertyTypes(ctx *NodeTypePropertyTypesContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypeSpecification.
	VisitEdgeTypeSpecification(ctx *EdgeTypeSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypePattern.
	VisitEdgeTypePattern(ctx *EdgeTypePatternContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypePhrase.
	VisitEdgeTypePhrase(ctx *EdgeTypePhraseContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypePhraseFiller.
	VisitEdgeTypePhraseFiller(ctx *EdgeTypePhraseFillerContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypeFiller.
	VisitEdgeTypeFiller(ctx *EdgeTypeFillerContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypeImpliedContent.
	VisitEdgeTypeImpliedContent(ctx *EdgeTypeImpliedContentContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypeKeyLabelSet.
	VisitEdgeTypeKeyLabelSet(ctx *EdgeTypeKeyLabelSetContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypeLabelSet.
	VisitEdgeTypeLabelSet(ctx *EdgeTypeLabelSetContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypePropertyTypes.
	VisitEdgeTypePropertyTypes(ctx *EdgeTypePropertyTypesContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypePatternDirected.
	VisitEdgeTypePatternDirected(ctx *EdgeTypePatternDirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypePatternPointingRight.
	VisitEdgeTypePatternPointingRight(ctx *EdgeTypePatternPointingRightContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypePatternPointingLeft.
	VisitEdgeTypePatternPointingLeft(ctx *EdgeTypePatternPointingLeftContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypePatternUndirected.
	VisitEdgeTypePatternUndirected(ctx *EdgeTypePatternUndirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#arcTypePointingRight.
	VisitArcTypePointingRight(ctx *ArcTypePointingRightContext) interface{}

	// Visit a parse tree produced by GQLParser#arcTypePointingLeft.
	VisitArcTypePointingLeft(ctx *ArcTypePointingLeftContext) interface{}

	// Visit a parse tree produced by GQLParser#arcTypeUndirected.
	VisitArcTypeUndirected(ctx *ArcTypeUndirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#sourceNodeTypeReference.
	VisitSourceNodeTypeReference(ctx *SourceNodeTypeReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#destinationNodeTypeReference.
	VisitDestinationNodeTypeReference(ctx *DestinationNodeTypeReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeKind.
	VisitEdgeKind(ctx *EdgeKindContext) interface{}

	// Visit a parse tree produced by GQLParser#endpointPairPhrase.
	VisitEndpointPairPhrase(ctx *EndpointPairPhraseContext) interface{}

	// Visit a parse tree produced by GQLParser#endpointPair.
	VisitEndpointPair(ctx *EndpointPairContext) interface{}

	// Visit a parse tree produced by GQLParser#endpointPairDirected.
	VisitEndpointPairDirected(ctx *EndpointPairDirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#endpointPairPointingRight.
	VisitEndpointPairPointingRight(ctx *EndpointPairPointingRightContext) interface{}

	// Visit a parse tree produced by GQLParser#endpointPairPointingLeft.
	VisitEndpointPairPointingLeft(ctx *EndpointPairPointingLeftContext) interface{}

	// Visit a parse tree produced by GQLParser#endpointPairUndirected.
	VisitEndpointPairUndirected(ctx *EndpointPairUndirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#connectorPointingRight.
	VisitConnectorPointingRight(ctx *ConnectorPointingRightContext) interface{}

	// Visit a parse tree produced by GQLParser#connectorUndirected.
	VisitConnectorUndirected(ctx *ConnectorUndirectedContext) interface{}

	// Visit a parse tree produced by GQLParser#sourceNodeTypeAlias.
	VisitSourceNodeTypeAlias(ctx *SourceNodeTypeAliasContext) interface{}

	// Visit a parse tree produced by GQLParser#destinationNodeTypeAlias.
	VisitDestinationNodeTypeAlias(ctx *DestinationNodeTypeAliasContext) interface{}

	// Visit a parse tree produced by GQLParser#labelSetPhrase.
	VisitLabelSetPhrase(ctx *LabelSetPhraseContext) interface{}

	// Visit a parse tree produced by GQLParser#labelSetSpecification.
	VisitLabelSetSpecification(ctx *LabelSetSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#propertyTypesSpecification.
	VisitPropertyTypesSpecification(ctx *PropertyTypesSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#propertyTypeList.
	VisitPropertyTypeList(ctx *PropertyTypeListContext) interface{}

	// Visit a parse tree produced by GQLParser#propertyType.
	VisitPropertyType(ctx *PropertyTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#propertyValueType.
	VisitPropertyValueType(ctx *PropertyValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingTableType.
	VisitBindingTableType(ctx *BindingTableTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#dynamicPropertyValueTypeLabel.
	VisitDynamicPropertyValueTypeLabel(ctx *DynamicPropertyValueTypeLabelContext) interface{}

	// Visit a parse tree produced by GQLParser#closedDynamicUnionTypeAtl1.
	VisitClosedDynamicUnionTypeAtl1(ctx *ClosedDynamicUnionTypeAtl1Context) interface{}

	// Visit a parse tree produced by GQLParser#closedDynamicUnionTypeAtl2.
	VisitClosedDynamicUnionTypeAtl2(ctx *ClosedDynamicUnionTypeAtl2Context) interface{}

	// Visit a parse tree produced by GQLParser#pathValueTypeLabel.
	VisitPathValueTypeLabel(ctx *PathValueTypeLabelContext) interface{}

	// Visit a parse tree produced by GQLParser#listValueTypeAlt3.
	VisitListValueTypeAlt3(ctx *ListValueTypeAlt3Context) interface{}

	// Visit a parse tree produced by GQLParser#listValueTypeAlt2.
	VisitListValueTypeAlt2(ctx *ListValueTypeAlt2Context) interface{}

	// Visit a parse tree produced by GQLParser#listValueTypeAlt1.
	VisitListValueTypeAlt1(ctx *ListValueTypeAlt1Context) interface{}

	// Visit a parse tree produced by GQLParser#predefinedTypeLabel.
	VisitPredefinedTypeLabel(ctx *PredefinedTypeLabelContext) interface{}

	// Visit a parse tree produced by GQLParser#recordTypeLabel.
	VisitRecordTypeLabel(ctx *RecordTypeLabelContext) interface{}

	// Visit a parse tree produced by GQLParser#openDynamicUnionTypeLabel.
	VisitOpenDynamicUnionTypeLabel(ctx *OpenDynamicUnionTypeLabelContext) interface{}

	// Visit a parse tree produced by GQLParser#typed.
	VisitTyped(ctx *TypedContext) interface{}

	// Visit a parse tree produced by GQLParser#predefinedType.
	VisitPredefinedType(ctx *PredefinedTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#booleanType.
	VisitBooleanType(ctx *BooleanTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#characterStringType.
	VisitCharacterStringType(ctx *CharacterStringTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#byteStringType.
	VisitByteStringType(ctx *ByteStringTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#minLength.
	VisitMinLength(ctx *MinLengthContext) interface{}

	// Visit a parse tree produced by GQLParser#maxLength.
	VisitMaxLength(ctx *MaxLengthContext) interface{}

	// Visit a parse tree produced by GQLParser#fixedLength.
	VisitFixedLength(ctx *FixedLengthContext) interface{}

	// Visit a parse tree produced by GQLParser#numericType.
	VisitNumericType(ctx *NumericTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#exactNumericType.
	VisitExactNumericType(ctx *ExactNumericTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#binaryExactNumericType.
	VisitBinaryExactNumericType(ctx *BinaryExactNumericTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#signedBinaryExactNumericType.
	VisitSignedBinaryExactNumericType(ctx *SignedBinaryExactNumericTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#unsignedBinaryExactNumericType.
	VisitUnsignedBinaryExactNumericType(ctx *UnsignedBinaryExactNumericTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#verboseBinaryExactNumericType.
	VisitVerboseBinaryExactNumericType(ctx *VerboseBinaryExactNumericTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#decimalExactNumericType.
	VisitDecimalExactNumericType(ctx *DecimalExactNumericTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#precision.
	VisitPrecision(ctx *PrecisionContext) interface{}

	// Visit a parse tree produced by GQLParser#scale.
	VisitScale(ctx *ScaleContext) interface{}

	// Visit a parse tree produced by GQLParser#approximateNumericType.
	VisitApproximateNumericType(ctx *ApproximateNumericTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#temporalType.
	VisitTemporalType(ctx *TemporalTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#temporalInstantType.
	VisitTemporalInstantType(ctx *TemporalInstantTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#datetimeType.
	VisitDatetimeType(ctx *DatetimeTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#localdatetimeType.
	VisitLocaldatetimeType(ctx *LocaldatetimeTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#dateType.
	VisitDateType(ctx *DateTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#timeType.
	VisitTimeType(ctx *TimeTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#localtimeType.
	VisitLocaltimeType(ctx *LocaltimeTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#temporalDurationType.
	VisitTemporalDurationType(ctx *TemporalDurationTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#temporalDurationQualifier.
	VisitTemporalDurationQualifier(ctx *TemporalDurationQualifierContext) interface{}

	// Visit a parse tree produced by GQLParser#referenceValueType.
	VisitReferenceValueType(ctx *ReferenceValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#immaterialValueType.
	VisitImmaterialValueType(ctx *ImmaterialValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#nullType.
	VisitNullType(ctx *NullTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#emptyType.
	VisitEmptyType(ctx *EmptyTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#graphReferenceValueType.
	VisitGraphReferenceValueType(ctx *GraphReferenceValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#closedGraphReferenceValueType.
	VisitClosedGraphReferenceValueType(ctx *ClosedGraphReferenceValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#openGraphReferenceValueType.
	VisitOpenGraphReferenceValueType(ctx *OpenGraphReferenceValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingTableReferenceValueType.
	VisitBindingTableReferenceValueType(ctx *BindingTableReferenceValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeReferenceValueType.
	VisitNodeReferenceValueType(ctx *NodeReferenceValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#closedNodeReferenceValueType.
	VisitClosedNodeReferenceValueType(ctx *ClosedNodeReferenceValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#openNodeReferenceValueType.
	VisitOpenNodeReferenceValueType(ctx *OpenNodeReferenceValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeReferenceValueType.
	VisitEdgeReferenceValueType(ctx *EdgeReferenceValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#closedEdgeReferenceValueType.
	VisitClosedEdgeReferenceValueType(ctx *ClosedEdgeReferenceValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#openEdgeReferenceValueType.
	VisitOpenEdgeReferenceValueType(ctx *OpenEdgeReferenceValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#pathValueType.
	VisitPathValueType(ctx *PathValueTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#listValueTypeName.
	VisitListValueTypeName(ctx *ListValueTypeNameContext) interface{}

	// Visit a parse tree produced by GQLParser#listValueTypeNameSynonym.
	VisitListValueTypeNameSynonym(ctx *ListValueTypeNameSynonymContext) interface{}

	// Visit a parse tree produced by GQLParser#recordType.
	VisitRecordType(ctx *RecordTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#fieldTypesSpecification.
	VisitFieldTypesSpecification(ctx *FieldTypesSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#fieldTypeList.
	VisitFieldTypeList(ctx *FieldTypeListContext) interface{}

	// Visit a parse tree produced by GQLParser#notNull.
	VisitNotNull(ctx *NotNullContext) interface{}

	// Visit a parse tree produced by GQLParser#fieldType.
	VisitFieldType(ctx *FieldTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#searchCondition.
	VisitSearchCondition(ctx *SearchConditionContext) interface{}

	// Visit a parse tree produced by GQLParser#predicate.
	VisitPredicate(ctx *PredicateContext) interface{}

	// Visit a parse tree produced by GQLParser#compOp.
	VisitCompOp(ctx *CompOpContext) interface{}

	// Visit a parse tree produced by GQLParser#existsPredicate.
	VisitExistsPredicate(ctx *ExistsPredicateContext) interface{}

	// Visit a parse tree produced by GQLParser#nullPredicate.
	VisitNullPredicate(ctx *NullPredicateContext) interface{}

	// Visit a parse tree produced by GQLParser#nullPredicatePart2.
	VisitNullPredicatePart2(ctx *NullPredicatePart2Context) interface{}

	// Visit a parse tree produced by GQLParser#valueTypePredicate.
	VisitValueTypePredicate(ctx *ValueTypePredicateContext) interface{}

	// Visit a parse tree produced by GQLParser#valueTypePredicatePart2.
	VisitValueTypePredicatePart2(ctx *ValueTypePredicatePart2Context) interface{}

	// Visit a parse tree produced by GQLParser#normalizedPredicatePart2.
	VisitNormalizedPredicatePart2(ctx *NormalizedPredicatePart2Context) interface{}

	// Visit a parse tree produced by GQLParser#directedPredicate.
	VisitDirectedPredicate(ctx *DirectedPredicateContext) interface{}

	// Visit a parse tree produced by GQLParser#directedPredicatePart2.
	VisitDirectedPredicatePart2(ctx *DirectedPredicatePart2Context) interface{}

	// Visit a parse tree produced by GQLParser#labeledPredicate.
	VisitLabeledPredicate(ctx *LabeledPredicateContext) interface{}

	// Visit a parse tree produced by GQLParser#labeledPredicatePart2.
	VisitLabeledPredicatePart2(ctx *LabeledPredicatePart2Context) interface{}

	// Visit a parse tree produced by GQLParser#isLabeledOrColon.
	VisitIsLabeledOrColon(ctx *IsLabeledOrColonContext) interface{}

	// Visit a parse tree produced by GQLParser#sourceDestinationPredicate.
	VisitSourceDestinationPredicate(ctx *SourceDestinationPredicateContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeReference.
	VisitNodeReference(ctx *NodeReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#sourcePredicatePart2.
	VisitSourcePredicatePart2(ctx *SourcePredicatePart2Context) interface{}

	// Visit a parse tree produced by GQLParser#destinationPredicatePart2.
	VisitDestinationPredicatePart2(ctx *DestinationPredicatePart2Context) interface{}

	// Visit a parse tree produced by GQLParser#edgeReference.
	VisitEdgeReference(ctx *EdgeReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#all_differentPredicate.
	VisitAll_differentPredicate(ctx *All_differentPredicateContext) interface{}

	// Visit a parse tree produced by GQLParser#samePredicate.
	VisitSamePredicate(ctx *SamePredicateContext) interface{}

	// Visit a parse tree produced by GQLParser#property_existsPredicate.
	VisitProperty_existsPredicate(ctx *Property_existsPredicateContext) interface{}

	// Visit a parse tree produced by GQLParser#conjunctiveExprAlt.
	VisitConjunctiveExprAlt(ctx *ConjunctiveExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#propertyGraphExprAlt.
	VisitPropertyGraphExprAlt(ctx *PropertyGraphExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#multDivExprAlt.
	VisitMultDivExprAlt(ctx *MultDivExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingTableExprAlt.
	VisitBindingTableExprAlt(ctx *BindingTableExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#signedExprAlt.
	VisitSignedExprAlt(ctx *SignedExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#isNotExprAlt.
	VisitIsNotExprAlt(ctx *IsNotExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#normalizedPredicateExprAlt.
	VisitNormalizedPredicateExprAlt(ctx *NormalizedPredicateExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#notExprAlt.
	VisitNotExprAlt(ctx *NotExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#valueFunctionExprAlt.
	VisitValueFunctionExprAlt(ctx *ValueFunctionExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#concatenationExprAlt.
	VisitConcatenationExprAlt(ctx *ConcatenationExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#disjunctiveExprAlt.
	VisitDisjunctiveExprAlt(ctx *DisjunctiveExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#comparisonExprAlt.
	VisitComparisonExprAlt(ctx *ComparisonExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#primaryExprAlt.
	VisitPrimaryExprAlt(ctx *PrimaryExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#addSubtractExprAlt.
	VisitAddSubtractExprAlt(ctx *AddSubtractExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#predicateExprAlt.
	VisitPredicateExprAlt(ctx *PredicateExprAltContext) interface{}

	// Visit a parse tree produced by GQLParser#valueFunction.
	VisitValueFunction(ctx *ValueFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#booleanValueExpression.
	VisitBooleanValueExpression(ctx *BooleanValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#characterOrByteStringFunction.
	VisitCharacterOrByteStringFunction(ctx *CharacterOrByteStringFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#subCharacterOrByteString.
	VisitSubCharacterOrByteString(ctx *SubCharacterOrByteStringContext) interface{}

	// Visit a parse tree produced by GQLParser#trimSingleCharacterOrByteString.
	VisitTrimSingleCharacterOrByteString(ctx *TrimSingleCharacterOrByteStringContext) interface{}

	// Visit a parse tree produced by GQLParser#foldCharacterString.
	VisitFoldCharacterString(ctx *FoldCharacterStringContext) interface{}

	// Visit a parse tree produced by GQLParser#trimMultiCharacterCharacterString.
	VisitTrimMultiCharacterCharacterString(ctx *TrimMultiCharacterCharacterStringContext) interface{}

	// Visit a parse tree produced by GQLParser#normalizeCharacterString.
	VisitNormalizeCharacterString(ctx *NormalizeCharacterStringContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeReferenceValueExpression.
	VisitNodeReferenceValueExpression(ctx *NodeReferenceValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeReferenceValueExpression.
	VisitEdgeReferenceValueExpression(ctx *EdgeReferenceValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#aggregatingValueExpression.
	VisitAggregatingValueExpression(ctx *AggregatingValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#valueExpressionPrimary.
	VisitValueExpressionPrimary(ctx *ValueExpressionPrimaryContext) interface{}

	// Visit a parse tree produced by GQLParser#parenthesizedValueExpression.
	VisitParenthesizedValueExpression(ctx *ParenthesizedValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#nonParenthesizedValueExpressionPrimary.
	VisitNonParenthesizedValueExpressionPrimary(ctx *NonParenthesizedValueExpressionPrimaryContext) interface{}

	// Visit a parse tree produced by GQLParser#nonParenthesizedValueExpressionPrimarySpecialCase.
	VisitNonParenthesizedValueExpressionPrimarySpecialCase(ctx *NonParenthesizedValueExpressionPrimarySpecialCaseContext) interface{}

	// Visit a parse tree produced by GQLParser#unsignedValueSpecification.
	VisitUnsignedValueSpecification(ctx *UnsignedValueSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#nonNegativeIntegerSpecification.
	VisitNonNegativeIntegerSpecification(ctx *NonNegativeIntegerSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#generalValueSpecification.
	VisitGeneralValueSpecification(ctx *GeneralValueSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#dynamicParameterSpecification.
	VisitDynamicParameterSpecification(ctx *DynamicParameterSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#letValueExpression.
	VisitLetValueExpression(ctx *LetValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#valueQueryExpression.
	VisitValueQueryExpression(ctx *ValueQueryExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#caseExpression.
	VisitCaseExpression(ctx *CaseExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#caseAbbreviation.
	VisitCaseAbbreviation(ctx *CaseAbbreviationContext) interface{}

	// Visit a parse tree produced by GQLParser#caseSpecification.
	VisitCaseSpecification(ctx *CaseSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#simpleCase.
	VisitSimpleCase(ctx *SimpleCaseContext) interface{}

	// Visit a parse tree produced by GQLParser#searchedCase.
	VisitSearchedCase(ctx *SearchedCaseContext) interface{}

	// Visit a parse tree produced by GQLParser#simpleWhenClause.
	VisitSimpleWhenClause(ctx *SimpleWhenClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#searchedWhenClause.
	VisitSearchedWhenClause(ctx *SearchedWhenClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#elseClause.
	VisitElseClause(ctx *ElseClauseContext) interface{}

	// Visit a parse tree produced by GQLParser#caseOperand.
	VisitCaseOperand(ctx *CaseOperandContext) interface{}

	// Visit a parse tree produced by GQLParser#whenOperandList.
	VisitWhenOperandList(ctx *WhenOperandListContext) interface{}

	// Visit a parse tree produced by GQLParser#whenOperand.
	VisitWhenOperand(ctx *WhenOperandContext) interface{}

	// Visit a parse tree produced by GQLParser#result.
	VisitResult(ctx *ResultContext) interface{}

	// Visit a parse tree produced by GQLParser#resultExpression.
	VisitResultExpression(ctx *ResultExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#castSpecification.
	VisitCastSpecification(ctx *CastSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#castOperand.
	VisitCastOperand(ctx *CastOperandContext) interface{}

	// Visit a parse tree produced by GQLParser#castTarget.
	VisitCastTarget(ctx *CastTargetContext) interface{}

	// Visit a parse tree produced by GQLParser#aggregateFunction.
	VisitAggregateFunction(ctx *AggregateFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#generalSetFunction.
	VisitGeneralSetFunction(ctx *GeneralSetFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#binarySetFunction.
	VisitBinarySetFunction(ctx *BinarySetFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#generalSetFunctionType.
	VisitGeneralSetFunctionType(ctx *GeneralSetFunctionTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#setQuantifier.
	VisitSetQuantifier(ctx *SetQuantifierContext) interface{}

	// Visit a parse tree produced by GQLParser#binarySetFunctionType.
	VisitBinarySetFunctionType(ctx *BinarySetFunctionTypeContext) interface{}

	// Visit a parse tree produced by GQLParser#dependentValueExpression.
	VisitDependentValueExpression(ctx *DependentValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#independentValueExpression.
	VisitIndependentValueExpression(ctx *IndependentValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#element_idFunction.
	VisitElement_idFunction(ctx *Element_idFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingVariableReference.
	VisitBindingVariableReference(ctx *BindingVariableReferenceContext) interface{}

	// Visit a parse tree produced by GQLParser#pathValueExpression.
	VisitPathValueExpression(ctx *PathValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#pathValueConstructor.
	VisitPathValueConstructor(ctx *PathValueConstructorContext) interface{}

	// Visit a parse tree produced by GQLParser#pathValueConstructorByEnumeration.
	VisitPathValueConstructorByEnumeration(ctx *PathValueConstructorByEnumerationContext) interface{}

	// Visit a parse tree produced by GQLParser#pathElementList.
	VisitPathElementList(ctx *PathElementListContext) interface{}

	// Visit a parse tree produced by GQLParser#pathElementListStart.
	VisitPathElementListStart(ctx *PathElementListStartContext) interface{}

	// Visit a parse tree produced by GQLParser#pathElementListStep.
	VisitPathElementListStep(ctx *PathElementListStepContext) interface{}

	// Visit a parse tree produced by GQLParser#listValueExpression.
	VisitListValueExpression(ctx *ListValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#listValueFunction.
	VisitListValueFunction(ctx *ListValueFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#trimListFunction.
	VisitTrimListFunction(ctx *TrimListFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#elementsFunction.
	VisitElementsFunction(ctx *ElementsFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#listValueConstructor.
	VisitListValueConstructor(ctx *ListValueConstructorContext) interface{}

	// Visit a parse tree produced by GQLParser#listValueConstructorByEnumeration.
	VisitListValueConstructorByEnumeration(ctx *ListValueConstructorByEnumerationContext) interface{}

	// Visit a parse tree produced by GQLParser#listElementList.
	VisitListElementList(ctx *ListElementListContext) interface{}

	// Visit a parse tree produced by GQLParser#listElement.
	VisitListElement(ctx *ListElementContext) interface{}

	// Visit a parse tree produced by GQLParser#recordConstructor.
	VisitRecordConstructor(ctx *RecordConstructorContext) interface{}

	// Visit a parse tree produced by GQLParser#fieldsSpecification.
	VisitFieldsSpecification(ctx *FieldsSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#fieldList.
	VisitFieldList(ctx *FieldListContext) interface{}

	// Visit a parse tree produced by GQLParser#field.
	VisitField(ctx *FieldContext) interface{}

	// Visit a parse tree produced by GQLParser#truthValue.
	VisitTruthValue(ctx *TruthValueContext) interface{}

	// Visit a parse tree produced by GQLParser#numericValueExpression.
	VisitNumericValueExpression(ctx *NumericValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#numericValueFunction.
	VisitNumericValueFunction(ctx *NumericValueFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#lengthExpression.
	VisitLengthExpression(ctx *LengthExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#cardinalityExpression.
	VisitCardinalityExpression(ctx *CardinalityExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#cardinalityExpressionArgument.
	VisitCardinalityExpressionArgument(ctx *CardinalityExpressionArgumentContext) interface{}

	// Visit a parse tree produced by GQLParser#charLengthExpression.
	VisitCharLengthExpression(ctx *CharLengthExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#byteLengthExpression.
	VisitByteLengthExpression(ctx *ByteLengthExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#pathLengthExpression.
	VisitPathLengthExpression(ctx *PathLengthExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#absoluteValueExpression.
	VisitAbsoluteValueExpression(ctx *AbsoluteValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#modulusExpression.
	VisitModulusExpression(ctx *ModulusExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#numericValueExpressionDividend.
	VisitNumericValueExpressionDividend(ctx *NumericValueExpressionDividendContext) interface{}

	// Visit a parse tree produced by GQLParser#numericValueExpressionDivisor.
	VisitNumericValueExpressionDivisor(ctx *NumericValueExpressionDivisorContext) interface{}

	// Visit a parse tree produced by GQLParser#trigonometricFunction.
	VisitTrigonometricFunction(ctx *TrigonometricFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#trigonometricFunctionName.
	VisitTrigonometricFunctionName(ctx *TrigonometricFunctionNameContext) interface{}

	// Visit a parse tree produced by GQLParser#generalLogarithmFunction.
	VisitGeneralLogarithmFunction(ctx *GeneralLogarithmFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#generalLogarithmBase.
	VisitGeneralLogarithmBase(ctx *GeneralLogarithmBaseContext) interface{}

	// Visit a parse tree produced by GQLParser#generalLogarithmArgument.
	VisitGeneralLogarithmArgument(ctx *GeneralLogarithmArgumentContext) interface{}

	// Visit a parse tree produced by GQLParser#commonLogarithm.
	VisitCommonLogarithm(ctx *CommonLogarithmContext) interface{}

	// Visit a parse tree produced by GQLParser#naturalLogarithm.
	VisitNaturalLogarithm(ctx *NaturalLogarithmContext) interface{}

	// Visit a parse tree produced by GQLParser#exponentialFunction.
	VisitExponentialFunction(ctx *ExponentialFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#powerFunction.
	VisitPowerFunction(ctx *PowerFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#numericValueExpressionBase.
	VisitNumericValueExpressionBase(ctx *NumericValueExpressionBaseContext) interface{}

	// Visit a parse tree produced by GQLParser#numericValueExpressionExponent.
	VisitNumericValueExpressionExponent(ctx *NumericValueExpressionExponentContext) interface{}

	// Visit a parse tree produced by GQLParser#squareRoot.
	VisitSquareRoot(ctx *SquareRootContext) interface{}

	// Visit a parse tree produced by GQLParser#floorFunction.
	VisitFloorFunction(ctx *FloorFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#ceilingFunction.
	VisitCeilingFunction(ctx *CeilingFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#characterStringValueExpression.
	VisitCharacterStringValueExpression(ctx *CharacterStringValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#byteStringValueExpression.
	VisitByteStringValueExpression(ctx *ByteStringValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#trimOperands.
	VisitTrimOperands(ctx *TrimOperandsContext) interface{}

	// Visit a parse tree produced by GQLParser#trimCharacterOrByteStringSource.
	VisitTrimCharacterOrByteStringSource(ctx *TrimCharacterOrByteStringSourceContext) interface{}

	// Visit a parse tree produced by GQLParser#trimSpecification.
	VisitTrimSpecification(ctx *TrimSpecificationContext) interface{}

	// Visit a parse tree produced by GQLParser#trimCharacterOrByteString.
	VisitTrimCharacterOrByteString(ctx *TrimCharacterOrByteStringContext) interface{}

	// Visit a parse tree produced by GQLParser#normalForm.
	VisitNormalForm(ctx *NormalFormContext) interface{}

	// Visit a parse tree produced by GQLParser#stringLength.
	VisitStringLength(ctx *StringLengthContext) interface{}

	// Visit a parse tree produced by GQLParser#datetimeValueExpression.
	VisitDatetimeValueExpression(ctx *DatetimeValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#datetimeValueFunction.
	VisitDatetimeValueFunction(ctx *DatetimeValueFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#dateFunction.
	VisitDateFunction(ctx *DateFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#timeFunction.
	VisitTimeFunction(ctx *TimeFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#localtimeFunction.
	VisitLocaltimeFunction(ctx *LocaltimeFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#datetimeFunction.
	VisitDatetimeFunction(ctx *DatetimeFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#localdatetimeFunction.
	VisitLocaldatetimeFunction(ctx *LocaldatetimeFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#dateFunctionParameters.
	VisitDateFunctionParameters(ctx *DateFunctionParametersContext) interface{}

	// Visit a parse tree produced by GQLParser#timeFunctionParameters.
	VisitTimeFunctionParameters(ctx *TimeFunctionParametersContext) interface{}

	// Visit a parse tree produced by GQLParser#datetimeFunctionParameters.
	VisitDatetimeFunctionParameters(ctx *DatetimeFunctionParametersContext) interface{}

	// Visit a parse tree produced by GQLParser#durationValueExpression.
	VisitDurationValueExpression(ctx *DurationValueExpressionContext) interface{}

	// Visit a parse tree produced by GQLParser#datetimeSubtraction.
	VisitDatetimeSubtraction(ctx *DatetimeSubtractionContext) interface{}

	// Visit a parse tree produced by GQLParser#datetimeSubtractionParameters.
	VisitDatetimeSubtractionParameters(ctx *DatetimeSubtractionParametersContext) interface{}

	// Visit a parse tree produced by GQLParser#datetimeValueExpression1.
	VisitDatetimeValueExpression1(ctx *DatetimeValueExpression1Context) interface{}

	// Visit a parse tree produced by GQLParser#datetimeValueExpression2.
	VisitDatetimeValueExpression2(ctx *DatetimeValueExpression2Context) interface{}

	// Visit a parse tree produced by GQLParser#durationValueFunction.
	VisitDurationValueFunction(ctx *DurationValueFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#durationFunction.
	VisitDurationFunction(ctx *DurationFunctionContext) interface{}

	// Visit a parse tree produced by GQLParser#durationFunctionParameters.
	VisitDurationFunctionParameters(ctx *DurationFunctionParametersContext) interface{}

	// Visit a parse tree produced by GQLParser#objectName.
	VisitObjectName(ctx *ObjectNameContext) interface{}

	// Visit a parse tree produced by GQLParser#objectNameOrBindingVariable.
	VisitObjectNameOrBindingVariable(ctx *ObjectNameOrBindingVariableContext) interface{}

	// Visit a parse tree produced by GQLParser#directoryName.
	VisitDirectoryName(ctx *DirectoryNameContext) interface{}

	// Visit a parse tree produced by GQLParser#schemaName.
	VisitSchemaName(ctx *SchemaNameContext) interface{}

	// Visit a parse tree produced by GQLParser#graphName.
	VisitGraphName(ctx *GraphNameContext) interface{}

	// Visit a parse tree produced by GQLParser#delimitedGraphName.
	VisitDelimitedGraphName(ctx *DelimitedGraphNameContext) interface{}

	// Visit a parse tree produced by GQLParser#graphTypeName.
	VisitGraphTypeName(ctx *GraphTypeNameContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeTypeName.
	VisitNodeTypeName(ctx *NodeTypeNameContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeTypeName.
	VisitEdgeTypeName(ctx *EdgeTypeNameContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingTableName.
	VisitBindingTableName(ctx *BindingTableNameContext) interface{}

	// Visit a parse tree produced by GQLParser#delimitedBindingTableName.
	VisitDelimitedBindingTableName(ctx *DelimitedBindingTableNameContext) interface{}

	// Visit a parse tree produced by GQLParser#procedureName.
	VisitProcedureName(ctx *ProcedureNameContext) interface{}

	// Visit a parse tree produced by GQLParser#labelName.
	VisitLabelName(ctx *LabelNameContext) interface{}

	// Visit a parse tree produced by GQLParser#propertyName.
	VisitPropertyName(ctx *PropertyNameContext) interface{}

	// Visit a parse tree produced by GQLParser#fieldName.
	VisitFieldName(ctx *FieldNameContext) interface{}

	// Visit a parse tree produced by GQLParser#elementVariable.
	VisitElementVariable(ctx *ElementVariableContext) interface{}

	// Visit a parse tree produced by GQLParser#pathVariable.
	VisitPathVariable(ctx *PathVariableContext) interface{}

	// Visit a parse tree produced by GQLParser#subpathVariable.
	VisitSubpathVariable(ctx *SubpathVariableContext) interface{}

	// Visit a parse tree produced by GQLParser#bindingVariable.
	VisitBindingVariable(ctx *BindingVariableContext) interface{}

	// Visit a parse tree produced by GQLParser#unsignedLiteral.
	VisitUnsignedLiteral(ctx *UnsignedLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#generalLiteral.
	VisitGeneralLiteral(ctx *GeneralLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#temporalLiteral.
	VisitTemporalLiteral(ctx *TemporalLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#dateLiteral.
	VisitDateLiteral(ctx *DateLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#timeLiteral.
	VisitTimeLiteral(ctx *TimeLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#datetimeLiteral.
	VisitDatetimeLiteral(ctx *DatetimeLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#listLiteral.
	VisitListLiteral(ctx *ListLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#recordLiteral.
	VisitRecordLiteral(ctx *RecordLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#identifier.
	VisitIdentifier(ctx *IdentifierContext) interface{}

	// Visit a parse tree produced by GQLParser#regularIdentifier.
	VisitRegularIdentifier(ctx *RegularIdentifierContext) interface{}

	// Visit a parse tree produced by GQLParser#timeZoneString.
	VisitTimeZoneString(ctx *TimeZoneStringContext) interface{}

	// Visit a parse tree produced by GQLParser#characterStringLiteral.
	VisitCharacterStringLiteral(ctx *CharacterStringLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#unsignedNumericLiteral.
	VisitUnsignedNumericLiteral(ctx *UnsignedNumericLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#exactNumericLiteral.
	VisitExactNumericLiteral(ctx *ExactNumericLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#approximateNumericLiteral.
	VisitApproximateNumericLiteral(ctx *ApproximateNumericLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#unsignedInteger.
	VisitUnsignedInteger(ctx *UnsignedIntegerContext) interface{}

	// Visit a parse tree produced by GQLParser#unsignedDecimalInteger.
	VisitUnsignedDecimalInteger(ctx *UnsignedDecimalIntegerContext) interface{}

	// Visit a parse tree produced by GQLParser#nullLiteral.
	VisitNullLiteral(ctx *NullLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#dateString.
	VisitDateString(ctx *DateStringContext) interface{}

	// Visit a parse tree produced by GQLParser#timeString.
	VisitTimeString(ctx *TimeStringContext) interface{}

	// Visit a parse tree produced by GQLParser#datetimeString.
	VisitDatetimeString(ctx *DatetimeStringContext) interface{}

	// Visit a parse tree produced by GQLParser#durationLiteral.
	VisitDurationLiteral(ctx *DurationLiteralContext) interface{}

	// Visit a parse tree produced by GQLParser#durationString.
	VisitDurationString(ctx *DurationStringContext) interface{}

	// Visit a parse tree produced by GQLParser#nodeSynonym.
	VisitNodeSynonym(ctx *NodeSynonymContext) interface{}

	// Visit a parse tree produced by GQLParser#edgesSynonym.
	VisitEdgesSynonym(ctx *EdgesSynonymContext) interface{}

	// Visit a parse tree produced by GQLParser#edgeSynonym.
	VisitEdgeSynonym(ctx *EdgeSynonymContext) interface{}

	// Visit a parse tree produced by GQLParser#nonReservedWords.
	VisitNonReservedWords(ctx *NonReservedWordsContext) interface{}

}